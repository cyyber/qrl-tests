//go:build e2e

package exited

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/slashingops"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const (
	primaryParticipant = 0
	exitingParticipant = 1

	exitTimeout     = 8 * time.Minute
	slashingTimeout = 4 * time.Minute
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Exited validator slashing E2E suite")
}

// An exited validator can be slashed until its withdrawable epoch, two epochs
// after its exit on the devnet. The suite submits the evidence as soon as the
// validator has exited, and the next block includes it.
var _ = ginkgo.Describe(
	"A validator that has exited but cannot withdraw yet",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "consensus", "slashing"),
	func() {
		var (
			client *beacon.Client
			val    rewards.Validator
			spec   slashingops.Spec
			signer validatorops.Signer
			// slashedBefore is what the lane's earlier suites slashed.
			slashedBefore []uint64
			// exitSigned is the epoch the validator signed its exit for, while
			// it was active; exited is its record once it has left.
			exitSigned   uint64
			exited       beacon.Validator
			slashingSlot uint64
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime := testsuite.LoadRuntime()
			nodes := testsuite.MustSucceed(runtime.Nodes(ctx))
			gomega.Expect(nodes).To(gomega.HaveLen(2), "the suite exits a validator of node 2")
			gomega.Expect(runtime.QrysmAlltoolsImage).NotTo(gomega.BeEmpty(), "Qrysm all-tools image is not configured")
			// The last of node 2's validators; its client stops attesting once
			// it has exited.
			candidates := runtime.Profile.ParticipantValidators(exitingParticipant)
			gomega.Expect(candidates).NotTo(gomega.BeEmpty(), "profile %q runs no validators on node 2", runtime.Profile)

			node := nodes[primaryParticipant]
			client = node.Beacon
			val = rewards.Validator{
				Client: client,
				Chain:  testsuite.MustSucceed(chaininfo.Load(ctx, client)),
				Spec:   testsuite.MustSucceed(rewards.LoadSpec(ctx, client)),
				Index:  candidates[len(candidates)-1],
			}
			slashingops.ExpectShortEpochs(val.Chain)
			spec = testsuite.MustSucceed(slashingops.LoadSpec(ctx, client))
			key := testsuite.MustSucceed(validatorops.GenesisKey(
				ctx, client, runtime.QrysmAlltoolsImage, node.Address.Hex(), val.Index))
			signer = testsuite.MustSucceed(validatorops.LoadSigner(ctx, client, key))
			slashedBefore = slashingops.SlashedIndices(ctx, client)
		}, ginkgo.NodeTimeout(3*time.Minute))

		ginkgo.It("exits after a voluntary exit it signs", func(ctx ginkgo.SpecContext) {
			// A genesis validator can exit once it has been active for the
			// shard committee period.
			period := testsuite.MustSucceed(client.SpecUint(ctx, "SHARD_COMMITTEE_PERIOD"))
			exitSigned = awaitEpoch(ctx, val, period)

			ginkgo.By("submitting a voluntary exit signed for epoch " + strconv.FormatUint(exitSigned, 10))
			exit := testsuite.MustSucceed(signer.SignVoluntaryExit(beacon.VoluntaryExit{Epoch: exitSigned, ValidatorIndex: val.Index}))
			gomega.Expect(client.SubmitVoluntaryExit(ctx, exit)).To(gomega.Succeed())
			var record beacon.Validator
			gomega.Eventually(func(g gomega.Gomega) {
				var err error
				record, err = client.Validator(ctx, strconv.FormatUint(val.Index, 10))
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(record.ExitEpoch).NotTo(gomega.Equal(beacon.FarFutureEpoch))
			}).WithContext(ctx).WithTimeout(2*val.Chain.EpochDuration).WithPolling(rewards.PollInterval).
				Should(gomega.Succeed(), "no block included the exit")

			ginkgo.By("waiting for its exit epoch " + strconv.FormatUint(record.ExitEpoch, 10))
			awaitEpoch(ctx, val, record.ExitEpoch)
			exited = testsuite.MustSucceed(client.Validator(ctx, strconv.FormatUint(val.Index, 10)))
			gomega.Expect(exited.Status).To(gomega.Equal("exited_unslashed"))
			ginkgo.GinkgoWriter.Printf("validator %d exited in epoch %d and can withdraw from epoch %d\n",
				val.Index, exited.ExitEpoch, exited.WithdrawableEpoch)
		}, ginkgo.SpecTimeout(exitTimeout))

		ginkgo.It("is slashed for a double vote it cast while active, before it can withdraw", func(ctx ginkgo.SpecContext) {
			// Two votes for the epoch it signed its exit in, with different
			// heads. A slashing needs only the two signatures, so the second
			// head is made up.
			slot := exitSigned * val.Chain.SlotsPerEpoch
			justified := testsuite.MustSucceed(client.FinalityCheckpointsAt(ctx, strconv.FormatUint(slot, 10)))
			target := beacon.Checkpoint{Epoch: exitSigned, Root: testsuite.MustSucceed(client.RootAtOrBefore(ctx, slot))}
			first := beacon.AttestationData{
				Slot: slot, BeaconBlockRoot: target.Root, Source: justified.CurrentJustified, Target: target,
			}
			second := first
			second.BeaconBlockRoot = "0x" + strings.Repeat("01", 32)
			slashing := testsuite.MustSucceed(slashingops.SignAttesterSlashing(signer, val.Index, first, second))

			head := testsuite.MustSucceed(client.HeadSlot(ctx))
			ginkgo.By("submitting a double vote of slot " + strconv.FormatUint(slot, 10) +
				" at head slot " + strconv.FormatUint(head, 10))
			gomega.Expect(client.SubmitAttesterSlashing(ctx, slashing)).To(gomega.Succeed())
			gomega.Expect(slashingops.AwaitSlashed(ctx, val)).To(gomega.BeTrue(), "the submitted slashing was not included")

			ginkgo.By("finding the evidence a block included")
			included, evidence := slashingops.FindEvidence(ctx, val, head)
			vote1, vote2 := slashingops.ExpectDoubleVoteEvidence(included, evidence)
			gomega.Expect([]beacon.AttestationData{vote1, vote2}).To(gomega.ConsistOf(first, second),
				"the votes the block at slot %d included", included)
			epoch := val.Chain.Epoch(included)
			gomega.Expect(epoch).To(gomega.BeNumerically(">=", exited.ExitEpoch), "the validator was slashed before it exited")
			gomega.Expect(epoch).To(gomega.BeNumerically("<", exited.WithdrawableEpoch), "the validator was slashed once it could withdraw")
			ginkgo.GinkgoWriter.Printf("slot %d, in epoch %d, included the double vote\n", included, epoch)
			slashingSlot = included
		}, ginkgo.SpecTimeout(slashingTimeout))

		ginkgo.It("loses the slashing penalty in the block that includes the evidence, which puts off its withdrawal",
			func(ctx ginkgo.SpecContext) {
				// ExpectSlashedInBlock checks the withdrawable epoch moves to a
				// whole slashings vector after the slashing.
				on := slashingops.ExpectSlashedInBlock(ctx, val, spec, slashingSlot, slashingops.DoubleVote)
				gomega.Expect(on.ExitEpoch).To(gomega.Equal(exited.ExitEpoch), "the slashing changed the exit epoch")
				gomega.Expect(on.WithdrawableEpoch).To(gomega.BeNumerically(">", exited.WithdrawableEpoch))
			}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("is the only validator the suite slashes, and the chain keeps finalizing", func(ctx ginkgo.SpecContext) {
			slashingops.ExpectOnlySlashed(ctx, client, append(slashedBefore, val.Index))
			testsuite.ExpectFinalizing(ctx, client, val.Chain)
		}, ginkgo.SpecTimeout(time.Minute))
	},
)

// awaitEpoch waits until the head reaches epoch, and returns the head's epoch.
func awaitEpoch(ctx context.Context, val rewards.Validator, epoch uint64) uint64 {
	ginkgo.GinkgoHelper()
	var reached uint64
	head := testsuite.MustSucceed(val.Client.HeadSlot(ctx))
	remaining := max(int64(epoch)-int64(val.Chain.Epoch(head)), 0)
	gomega.Eventually(func(g gomega.Gomega) {
		head, err := val.Client.HeadSlot(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		reached = val.Chain.Epoch(head)
		g.Expect(reached).To(gomega.BeNumerically(">=", epoch))
	}).WithContext(ctx).
		WithTimeout(time.Duration(remaining+2)*val.Chain.EpochDuration).
		WithPolling(time.Second).
		Should(gomega.Succeed(), "the head did not reach epoch %d", epoch)
	return reached
}
