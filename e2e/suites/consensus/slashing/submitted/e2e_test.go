//go:build e2e

package submitted

import (
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

	inclusionTimeout = 5 * time.Minute
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Submitted proposer slashing E2E suite")
}

var _ = ginkgo.Describe(
	"A proposer slashing submitted through the beacon API",
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
			node := testsuite.MustSucceed(runtime.PrimaryNode(ctx))
			gomega.Expect(node.QrysmAlltoolsImage).NotTo(gomega.BeEmpty(), "Qrysm all-tools image is not configured")
			// The last of node 1's validators. Its client keeps running: the
			// evidence is made up, so nothing the client signs plays a part.
			candidates := runtime.Profile.ParticipantValidators(primaryParticipant)
			gomega.Expect(candidates).NotTo(gomega.BeEmpty(), "profile %q runs no validators on node 1", runtime.Profile)

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
				ctx, client, node.QrysmAlltoolsImage, node.Address.Hex(), val.Index))
			signer = testsuite.MustSucceed(validatorops.LoadSigner(ctx, client, key))
			slashedBefore = slashingops.SlashedIndices(ctx, client)
		}, ginkgo.NodeTimeout(3*time.Minute))

		ginkgo.It("is included in a block, which slashes the validator", func(ctx ginkgo.SpecContext) {
			// Two headers the validator signed for the head slot. Nothing
			// checks a slashing's headers against the chain, nor that the
			// validator was due to propose, so their roots are made up.
			head := testsuite.MustSucceed(client.BlockHeader(ctx, "head"))
			first := beacon.BeaconBlockHeaderMessage{
				Slot: head.Slot, ProposerIndex: val.Index,
				ParentRoot: filledRoot("00"), StateRoot: filledRoot("00"), BodyRoot: filledRoot("01"),
			}
			second := first
			second.BodyRoot = filledRoot("02")
			slashing := testsuite.MustSucceed(slashingops.SignProposerSlashing(signer, val.Chain.SlotsPerEpoch, first, second))

			ginkgo.By("submitting two headers validator " + strconv.FormatUint(val.Index, 10) +
				" signed for slot " + strconv.FormatUint(head.Slot, 10))
			gomega.Expect(client.SubmitProposerSlashing(ctx, slashing)).To(gomega.Succeed())
			gomega.Expect(slashingops.AwaitSlashed(ctx, val)).To(gomega.BeTrue(), "the submitted slashing was not included")

			ginkgo.By("finding the evidence a block included")
			slot, evidence := slashingops.FindEvidence(ctx, val, head.Slot)
			included1, included2 := slashingops.ExpectDoubleProposalEvidence(slot, evidence)
			gomega.Expect([]beacon.BeaconBlockHeaderMessage{included1, included2}).To(gomega.ConsistOf(first, second),
				"the headers the block at slot %d included", slot)
			ginkgo.GinkgoWriter.Printf("slot %d included the slashing submitted at head slot %d\n", slot, head.Slot)
			slashingSlot = slot
		}, ginkgo.SpecTimeout(inclusionTimeout))

		ginkgo.It("loses the slashing penalty in the block that includes the evidence, whose proposer earns the whistleblower reward",
			func(ctx ginkgo.SpecContext) {
				slashingops.ExpectSlashedInBlock(ctx, val, spec, slashingSlot, slashingops.DoubleProposal)
			}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("is the only validator the suite slashes, and the chain keeps finalizing", func(ctx ginkgo.SpecContext) {
			slashingops.ExpectOnlySlashed(ctx, client, append(slashedBefore, val.Index))
			testsuite.ExpectFinalizing(ctx, client, val.Chain)
		}, ginkgo.SpecTimeout(time.Minute))
	},
)

// filledRoot is a 0x-prefixed root of one repeated byte, given in hex.
func filledRoot(byteHex string) string {
	return "0x" + strings.Repeat(byteHex, 32)
}
