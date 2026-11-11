//go:build e2e

package surroundvote

import (
	"strconv"
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
	publisherParticipant = 0

	justifiedTimeout = 4 * time.Minute
	surroundTimeout  = 10 * time.Minute
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Surround-vote slashing E2E suite")
}

// The suite signs the votes of a validator no client runs, so no vote of its
// own client gets in the way, and publishes them through node 1's beacon API.
// A beacon node feeds its slasher only the attestations it receives from
// peers, so node 2's slasher, which receives them through gossip, is the one
// that detects the surround vote.
var _ = ginkgo.Describe(
	"A validator whose vote surrounds one of its earlier votes",
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
			slashingSlot  uint64
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime := testsuite.LoadRuntime()
			nodes := testsuite.MustSucceed(runtime.Nodes(ctx))
			gomega.Expect(nodes).To(gomega.HaveLen(2), "the suite publishes on one node for the other's slasher")
			gomega.Expect(runtime.QrysmAlltoolsImage).NotTo(gomega.BeEmpty(), "Qrysm all-tools image is not configured")
			spares := runtime.Profile.SpareValidators()
			gomega.Expect(spares).NotTo(gomega.BeEmpty(), "profile %q has no genesis validator that no client runs", runtime.Profile)

			publisher := nodes[publisherParticipant]
			client = publisher.Beacon
			val = rewards.Validator{
				Client: client,
				Chain:  testsuite.MustSucceed(chaininfo.Load(ctx, client)),
				Spec:   testsuite.MustSucceed(rewards.LoadSpec(ctx, client)),
				Index:  spares[0],
			}
			slashingops.ExpectShortEpochs(val.Chain)
			spec = testsuite.MustSucceed(slashingops.LoadSpec(ctx, client))
			key := testsuite.MustSucceed(validatorops.GenesisKey(
				ctx, client, runtime.QrysmAlltoolsImage, publisher.Address.Hex(), val.Index))
			signer = testsuite.MustSucceed(validatorops.LoadSigner(ctx, client, key))
			slashedBefore = slashingops.SlashedIndices(ctx, client)
		}, ginkgo.NodeTimeout(3*time.Minute))

		ginkgo.It("is slashed once a peer's slasher receives both votes through gossip", func(ctx ginkgo.SpecContext) {
			// The surrounding vote's source precedes the justified checkpoint
			// the surrounded vote starts from.
			ginkgo.By("waiting for a justified checkpoint after genesis")
			gomega.Eventually(func(g gomega.Gomega) {
				checkpoints, err := client.FinalityCheckpoints(ctx)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(checkpoints.CurrentJustified.Epoch).To(gomega.BeNumerically(">=", 1))
			}).WithContext(ctx).WithTimeout(justifiedTimeout).WithPolling(rewards.PollInterval).Should(gomega.Succeed())

			// Each vote takes the validator's real committee seat of its target
			// epoch, which gossip checks.
			head := testsuite.MustSucceed(client.HeadSlot(ctx))
			epoch := max(val.Chain.EpochAt(time.Now()), val.Chain.Epoch(head))
			innerDuty := attesterDuty(ctx, client, epoch, val.Index)
			outerDuty := attesterDuty(ctx, client, epoch+1, val.Index)

			ginkgo.By("publishing the vote of slot " + strconv.FormatUint(innerDuty.Slot, 10) +
				", from the justified checkpoint to epoch " + strconv.FormatUint(epoch, 10))
			awaitVote(ctx, val.Chain, innerDuty)
			// The head state's justified checkpoint, as an honest vote takes it.
			justified := testsuite.MustSucceed(client.FinalityCheckpoints(ctx))
			inner := headVote(ctx, client, val.Chain, innerDuty, justified.CurrentJustified)
			publish(ctx, client, signer, innerDuty, inner)

			ginkgo.By("publishing the vote of slot " + strconv.FormatUint(outerDuty.Slot, 10) +
				", from an older checkpoint to epoch " + strconv.FormatUint(epoch+1, 10))
			awaitVote(ctx, val.Chain, outerDuty)
			older := inner.Source.Epoch - 1
			source := beacon.Checkpoint{
				Epoch: older,
				Root:  testsuite.MustSucceed(client.RootAtOrBefore(ctx, older*val.Chain.SlotsPerEpoch)),
			}
			outer := headVote(ctx, client, val.Chain, outerDuty, source)
			gomega.Expect(slashingops.IsSurroundVote(outer, inner)).To(gomega.BeTrue(), "the second vote surrounds the first")
			publish(ctx, client, signer, outerDuty, outer)

			gomega.Expect(slashingops.AwaitSlashed(ctx, val)).To(gomega.BeTrue(), "no slasher detected the surround vote")

			ginkgo.By("finding the evidence a block included")
			slot, evidence := slashingops.FindEvidence(ctx, val, innerDuty.Slot)
			first, second := slashingops.ExpectSurroundVoteEvidence(slot, evidence)
			gomega.Expect([]beacon.AttestationData{first, second}).To(gomega.ConsistOf(inner, outer),
				"the votes the block at slot %d included", slot)
			ginkgo.GinkgoWriter.Printf("slot %d included the surround vote of slots %d and %d\n", slot, first.Slot, second.Slot)
			slashingSlot = slot
		}, ginkgo.SpecTimeout(surroundTimeout))

		ginkgo.It("loses the slashing penalty in the block that includes the evidence, whose proposer earns the whistleblower reward",
			func(ctx ginkgo.SpecContext) {
				slashingops.ExpectSlashedInBlock(ctx, val, spec, slashingSlot, slashingops.SurroundVote)
			}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("is the only validator the suite slashes, and the chain keeps finalizing", func(ctx ginkgo.SpecContext) {
			slashingops.ExpectOnlySlashed(ctx, client, append(slashedBefore, val.Index))
			testsuite.ExpectFinalizing(ctx, client, val.Chain)
		}, ginkgo.SpecTimeout(time.Minute))
	},
)
