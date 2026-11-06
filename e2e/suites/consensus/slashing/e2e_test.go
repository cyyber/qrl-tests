//go:build e2e

package slashing

import (
	"strconv"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const (
	doubleVoteAttempts = 3

	attestTimeout     = 5 * time.Minute
	doubleVoteTimeout = 15 * time.Minute
	exitTimeout       = 5 * time.Minute

	afterExitEpochs  = 2
	afterExitTimeout = 4 * time.Minute
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Slashing E2E suite")
}

// The suite follows the slashed validator through its exit and checks its
// withdrawable epoch, but waits for neither its correlation penalty nor its
// withdrawal: they fall due 256 and 512 epochs after the slashing, hours on
// the mainnet preset. Following them is future work, for a lane on images
// built with the minimal preset, whose 64-epoch slashings vector brings them
// within an hour.
var _ = ginkgo.Describe(
	"A validator whose key runs on two nodes at once",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "consensus", "slashing"),
	func() {
		var (
			// primary runs the validator from the start; duplicate is a
			// second copy of its key, on the node the suite pauses.
			primary, duplicate      *node
			val                     rewards.Validator
			spec                    slashingSpec
			keystores               []sidecar.File
			pausedNodeValidators    []uint64
			slashingSlot, firstVote uint64
			onSlashing              beacon.Validator
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime := testsuite.LoadRuntime()
			nodes := testsuite.MustSucceed(runtime.Nodes(ctx))
			gomega.Expect(nodes).To(gomega.HaveLen(2), "the suite runs the validator on two nodes")
			for _, node := range nodes {
				gomega.Expect(node.BeaconGRPC).NotTo(gomega.BeEmpty(), "beacon gRPC is not published")
			}
			gomega.Expect(runtime.ValidatorImage).NotTo(gomega.BeEmpty(), "validator image is not configured")
			gomega.Expect(runtime.QrysmAlltoolsImage).NotTo(gomega.BeEmpty(), "Qrysm all-tools image is not configured")
			primary = &node{Node: nodes[0], graffiti: "primary"}
			duplicate = &node{Node: nodes[1], graffiti: "duplicate"}
			pausedNodeValidators = runtime.Profile.ParticipantValidators(1)

			spares := runtime.Profile.SpareValidators()
			gomega.Expect(spares).NotTo(gomega.BeEmpty(), "profile %q has no genesis validator that no client runs", runtime.Profile)
			beaconClient := primary.Beacon
			val = rewards.Validator{
				Client: beaconClient,
				Chain:  testsuite.MustSucceed(chaininfo.Load(ctx, beaconClient)),
				Spec:   testsuite.MustSucceed(rewards.LoadSpec(ctx, beaconClient)),
				Index:  spares[0],
			}
			spec = testsuite.MustSucceed(loadSlashingSpec(ctx, beaconClient))

			keystores = testsuite.MustSucceed(validatorops.GenesisKeystores(
				ctx, beaconClient, runtime.QrysmAlltoolsImage, primary.Address.Hex(), val.Index))
		}, ginkgo.NodeTimeout(3*time.Minute))

		ginkgo.AfterEach(func() {
			for _, node := range []*node{primary, duplicate} {
				if node != nil {
					testsuite.ReportLogsOnFailure(node.graffiti+" validator client", node.client)
				}
			}
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(primary.stopClient()).To(gomega.Succeed())
			gomega.Expect(duplicate.stopClient()).To(gomega.Succeed())
		})

		ginkgo.It("attests through its validator client", func(ctx ginkgo.SpecContext) {
			head := testsuite.MustSucceed(primary.Beacon.HeadSlot(ctx))
			primary.startClient(ctx, keystores)
			val.FirstAttestedEpoch(ctx, val.Chain.Epoch(head), attestTimeout)
		}, ginkgo.SpecTimeout(attestTimeout+time.Minute))

		ginkgo.It("is slashed for a double vote once a second client runs its key on a node that sees a block late",
			func(ctx ginkgo.SpecContext) {
				scenario := doubleVote{
					val:                  val,
					duplicate:            duplicate,
					pausedNodeValidators: pausedNodeValidators,
					keystores:            keystores,
				}
				for attempt := 1; ; attempt++ {
					gomega.Expect(attempt).To(gomega.BeNumerically("<=", doubleVoteAttempts),
						"no double vote was slashed after %d delayed blocks", doubleVoteAttempts)
					slot, run := scenario.attempt(ctx)
					if firstVote == 0 {
						firstVote = slot
					}
					if run && scenario.awaitSlashed(ctx) {
						break
					}
				}

				ginkgo.By("finding the evidence a block included")
				slot, evidence := scenario.findEvidence(ctx, firstVote)
				gomega.Expect(evidence.proposer).To(gomega.BeNil(),
					"the block at slot %d slashed the validator for a double proposal", slot)
				gomega.Expect(evidence.attester).NotTo(gomega.BeNil())
				first, second := evidence.attester.Attestation1.Data, evidence.attester.Attestation2.Data
				gomega.Expect(isDoubleVote(first, second)).To(gomega.BeTrue(),
					"the attestations of slots %d and %d are not a double vote", first.Slot, second.Slot)
				ginkgo.GinkgoWriter.Printf("slot %d included a double vote of slot %d for heads %s and %s\n",
					slot, first.Slot, first.BeaconBlockRoot, second.BeaconBlockRoot)
				slashingSlot = slot
			}, ginkgo.SpecTimeout(doubleVoteTimeout))

		ginkgo.It("pays the slashing penalty in the block that includes the evidence, and its proposer the whistleblower reward",
			func(ctx ginkgo.SpecContext) {
				onSlashing = expectSlashedInBlock(ctx, val, spec, slashingSlot)
			}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("loses exactly the missed-vote penalty in every epoch from its slashing through its exit",
			func(ctx ginkgo.SpecContext) {
				// Its votes no longer count, whatever its clients sign.
				exit := onSlashing.ExitEpoch
				val.AwaitRewards(ctx, exit)
				for epoch := val.Chain.Epoch(slashingSlot) + 1; epoch <= exit; epoch++ {
					expectMissedVotePenalty(ctx, val, epoch)
				}

				exitState := strconv.FormatUint(exit*val.Chain.SlotsPerEpoch, 10)
				exited := testsuite.MustSucceed(val.Client.ValidatorAt(ctx, exitState, strconv.FormatUint(val.Index, 10)))
				gomega.Expect(exited.Status).To(gomega.Equal("exited_slashed"), "status at the start of epoch %d", exit)
			}, ginkgo.SpecTimeout(exitTimeout))

		ginkgo.It("keeps losing the missed-vote penalty after its exit, with its stake locked until it can withdraw",
			func(ctx ginkgo.SpecContext) {
				// A slashed validator stays liable for missed votes until it
				// can withdraw.
				first := onSlashing.ExitEpoch + 1
				last := onSlashing.ExitEpoch + afterExitEpochs
				val.AwaitRewards(ctx, last)
				index := strconv.FormatUint(val.Index, 10)
				for epoch := first; epoch <= last; epoch++ {
					expectMissedVotePenalty(ctx, val, epoch)

					state := strconv.FormatUint((epoch+2)*val.Chain.SlotsPerEpoch, 10)
					record := testsuite.MustSucceed(val.Client.ValidatorAt(ctx, state, index))
					gomega.Expect(record.Status).To(gomega.Equal("exited_slashed"), "status at the start of epoch %d", epoch+2)
					gomega.Expect(record.WithdrawableEpoch).To(gomega.Equal(onSlashing.WithdrawableEpoch),
						"withdrawable epoch at the start of epoch %d", epoch+2)
					gomega.Expect(record.Balance).To(gomega.BeNumerically(">", 0), "balance at the start of epoch %d", epoch+2)
				}
			}, ginkgo.SpecTimeout(afterExitTimeout))

		ginkgo.It("slashes no other validator, and the chain keeps finalizing", func(ctx ginkgo.SpecContext) {
			slashed := testsuite.MustSucceed(val.Client.SlashedValidators(ctx))
			indices := make([]uint64, len(slashed))
			for position, record := range slashed {
				indices[position] = record.Index
			}
			gomega.Expect(indices).To(gomega.Equal([]uint64{val.Index}))

			// A healthy chain finalizes within three epochs of its head.
			head := testsuite.MustSucceed(val.Client.HeadSlot(ctx))
			checkpoints := testsuite.MustSucceed(val.Client.FinalityCheckpoints(ctx))
			gomega.Expect(checkpoints.Finalized.Epoch+3).To(gomega.BeNumerically(">=", val.Chain.Epoch(head)),
				"finalized epoch %d at head slot %d", checkpoints.Finalized.Epoch, head)
		}, ginkgo.SpecTimeout(time.Minute))
	},
)
