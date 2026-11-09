//go:build e2e

package doublevote

import (
	"errors"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/e2e/internal/slashingops"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const (
	doubleVoteAttempts = 3

	attestTimeout     = 5 * time.Minute
	doubleVoteTimeout = 15 * time.Minute
	exitTimeout       = 7 * time.Minute

	afterExitEpochs  = 2
	afterExitTimeout = 4 * time.Minute
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Double-vote slashing E2E suite")
}

// The suite follows the slashed validator through its exit and checks its
// withdrawable epoch, but waits for neither its correlation penalty nor its
// withdrawal, which take hours on the mainnet preset. Following them is future
// work, for a lane on minimal-preset images.
var _ = ginkgo.Describe(
	"A validator whose key runs on two nodes at once",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "consensus", "slashing"),
	func() {
		var (
			// primary runs the validator from the start; duplicate is a
			// second copy of its key, on the node the suite pauses.
			primary, duplicate      *slashingops.Node
			val                     rewards.Validator
			spec                    slashingops.Spec
			keystores               []sidecar.File
			duplicateNodeValidators []uint64
			slashingSlot            uint64
			onSlashing              beacon.Validator
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime := testsuite.LoadRuntime()
			nodes := testsuite.MustSucceed(runtime.Nodes(ctx))
			gomega.Expect(nodes).To(gomega.HaveLen(2), "the suite runs the validator on two nodes")
			for _, participant := range nodes {
				gomega.Expect(participant.BeaconGRPC).NotTo(gomega.BeEmpty(), "beacon gRPC is not published")
			}
			gomega.Expect(runtime.ValidatorImage).NotTo(gomega.BeEmpty(), "validator image is not configured")
			gomega.Expect(runtime.QrysmAlltoolsImage).NotTo(gomega.BeEmpty(), "Qrysm all-tools image is not configured")
			primary = &slashingops.Node{Node: nodes[slashingops.PrimaryParticipant], Graffiti: "primary"}
			duplicate = &slashingops.Node{Node: nodes[slashingops.DuplicateParticipant], Graffiti: "duplicate"}
			duplicateNodeValidators = runtime.Profile.ParticipantValidators(slashingops.DuplicateParticipant)

			spares := runtime.Profile.SpareValidators()
			gomega.Expect(spares).NotTo(gomega.BeEmpty(), "profile %q has no genesis validator that no client runs", runtime.Profile)
			beaconClient := primary.Beacon
			val = rewards.Validator{
				Client: beaconClient,
				Chain:  testsuite.MustSucceed(chaininfo.Load(ctx, beaconClient)),
				Spec:   testsuite.MustSucceed(rewards.LoadSpec(ctx, beaconClient)),
				Index:  spares[0],
			}
			slashingops.ExpectShortEpochs(val.Chain)
			spec = testsuite.MustSucceed(slashingops.LoadSpec(ctx, beaconClient))

			keystores = testsuite.MustSucceed(validatorops.GenesisKeystores(
				ctx, beaconClient, runtime.QrysmAlltoolsImage, primary.Address.Hex(), val.Index))
		}, ginkgo.NodeTimeout(3*time.Minute))

		// Each double-vote attempt stops the duplicate's client and prints its
		// logs, so only the primary's are left to report.
		ginkgo.AfterEach(func() {
			primary.ReportLogsOnFailure()
		})

		ginkgo.AfterAll(func() {
			// Stop both clients even when one fails to stop.
			gomega.Expect(errors.Join(primary.StopClient(), duplicate.StopClient())).To(gomega.Succeed())
		})

		ginkgo.It("attests through its validator client", func(ctx ginkgo.SpecContext) {
			head := testsuite.MustSucceed(primary.Beacon.HeadSlot(ctx))
			primary.StartClient(ctx, keystores)
			val.FirstAttestedEpoch(ctx, val.Chain.Epoch(head), attestTimeout)
		}, ginkgo.SpecTimeout(attestTimeout+time.Minute))

		ginkgo.It("is slashed for a double vote once a second client runs its key on a node that sees a block late",
			func(ctx ginkgo.SpecContext) {
				scenario := doubleVote{
					val:                  val,
					duplicate:            duplicate,
					pausedNodeValidators: duplicateNodeValidators,
					keystores:            keystores,
				}
				// An earlier attempt's double vote can be slashed during a
				// later attempt, so the evidence search starts from the first
				// attempt's vote.
				var firstVote uint64
				slashed := false
				for attempt := 1; attempt <= doubleVoteAttempts && !slashed; attempt++ {
					slot, run := scenario.attempt(ctx)
					if attempt == 1 {
						firstVote = slot
					}
					slashed = run && slashingops.AwaitSlashed(ctx, val)
				}
				gomega.Expect(slashed).To(gomega.BeTrue(), "no double vote was slashed in %d attempts", doubleVoteAttempts)

				ginkgo.By("finding the evidence a block included")
				slot, evidence := slashingops.FindEvidence(ctx, val, firstVote)
				first, second := slashingops.ExpectDoubleVoteEvidence(slot, evidence)
				ginkgo.GinkgoWriter.Printf("slot %d included a double vote of slot %d for heads %s and %s\n",
					slot, first.Slot, first.BeaconBlockRoot, second.BeaconBlockRoot)
				slashingSlot = slot
			}, ginkgo.SpecTimeout(doubleVoteTimeout))

		ginkgo.It("loses the slashing penalty in the block that includes the evidence, whose proposer earns the whistleblower reward",
			func(ctx ginkgo.SpecContext) {
				onSlashing = slashingops.ExpectSlashedInBlock(ctx, val, spec, slashingSlot, slashingops.DoubleVote)
			}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("loses exactly the missed-vote penalty in every epoch from its slashing through its exit",
			func(ctx ginkgo.SpecContext) {
				// Its votes no longer count, whatever its clients sign. The
				// slashing epoch's rewards are processed an epoch later, by
				// when the validator is slashed, so they are penalties too.
				exit := onSlashing.ExitEpoch
				val.AwaitRewards(ctx, exit)
				for epoch := val.Chain.Epoch(slashingSlot); epoch <= exit; epoch++ {
					expectMissedVotePenalty(ctx, val, epoch)
				}
				expectExitedSlashed(ctx, val, exit, onSlashing)
			}, ginkgo.SpecTimeout(exitTimeout))

		ginkgo.It("keeps losing the missed-vote penalty after its exit, before it can withdraw",
			func(ctx ginkgo.SpecContext) {
				// A slashed validator stays liable for missed votes until it
				// can withdraw, hundreds of epochs after its exit. The spec
				// checks the first afterExitEpochs of them.
				first := onSlashing.ExitEpoch + 1
				last := onSlashing.ExitEpoch + afterExitEpochs
				gomega.Expect(last+2).To(gomega.BeNumerically("<", onSlashing.WithdrawableEpoch),
					"the validator can withdraw by epoch %d", last+2)
				val.AwaitRewards(ctx, last)
				for epoch := first; epoch <= last; epoch++ {
					expectMissedVotePenalty(ctx, val, epoch)
				}
			}, ginkgo.SpecTimeout(afterExitTimeout))

		ginkgo.It("is the only validator slashed, and the chain keeps finalizing", func(ctx ginkgo.SpecContext) {
			slashingops.ExpectOnlySlashed(ctx, val.Client, []uint64{val.Index})
			slashingops.ExpectFinalizing(ctx, val.Client, val.Chain)
		}, ginkgo.SpecTimeout(time.Minute))
	},
)
