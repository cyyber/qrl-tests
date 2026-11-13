//go:build e2e

package doubleproposal

import (
	"net/netip"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/slashingops"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const (
	primaryParticipant   = 0
	duplicateParticipant = 1
	watcherParticipant   = 2

	doubleProposalAttempts = 3
	doubleProposalTimeout  = 15 * time.Minute
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Double-proposal slashing E2E suite")
}

var _ = ginkgo.Describe(
	"A validator whose key runs on two nodes when it proposes",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "consensus", "slashing"),
	func() {
		var (
			// duplicate runs a second copy of one of node 1's keys, in a sidecar
			// attached to node 2's beacon node, with its own graffiti so the
			// two blocks differ.
			duplicate    *slashingops.Node
			scenario     *doubleProposal
			client       *beacon.Client
			chain        chaininfo.Info
			rewardsSpec  rewards.Spec
			spec         slashingops.Spec
			candidates   []uint64
			val          rewards.Validator
			proposalSlot uint64
			slashingSlot uint64
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime := testsuite.LoadRuntime()
			nodes := testsuite.MustSucceed(runtime.Nodes(ctx))
			gomega.Expect(nodes).To(gomega.HaveLen(3), "the suite proposes on two nodes, and a third watches")
			gomega.Expect(nodes[duplicateParticipant].BeaconGRPC).NotTo(gomega.BeEmpty(), "beacon gRPC is not published")
			gomega.Expect(runtime.ValidatorImage).NotTo(gomega.BeEmpty(), "validator image is not configured")
			gomega.Expect(runtime.QrysmAlltoolsImage).NotTo(gomega.BeEmpty(), "Qrysm all-tools image is not configured")
			duplicate = &slashingops.Node{Node: nodes[duplicateParticipant], Graffiti: "duplicate-proposer"}
			scenario = &doubleProposal{
				duplicate: duplicate,
				peers: []netip.Addr{
					beaconAddress(nodes[primaryParticipant]), beaconAddress(nodes[watcherParticipant]),
				},
				alltoolsImage:       runtime.QrysmAlltoolsImage,
				withdrawalRecipient: nodes[primaryParticipant].Address.Hex(),
			}
			candidates = runtime.Profile.ParticipantValidators(primaryParticipant)
			gomega.Expect(candidates).NotTo(gomega.BeEmpty(), "profile %q runs no validators on node 1", runtime.Profile)

			client = nodes[primaryParticipant].Beacon
			chain = testsuite.MustSucceed(chaininfo.Load(ctx, client))
			testsuite.ExpectShortEpochs(chain)
			rewardsSpec = testsuite.MustSucceed(rewards.LoadSpec(ctx, client))
			spec = testsuite.MustSucceed(slashingops.LoadSpec(ctx, client))
			gomega.Expect(devnet.PrepareIsolation(ctx)).To(gomega.Succeed())
		}, ginkgo.NodeTimeout(3*time.Minute))

		ginkgo.AfterAll(func() {
			gomega.Expect(duplicate.StopClient()).To(gomega.Succeed())
		})

		ginkgo.It("is slashed for a double proposal once the two nodes don't see each other's block in time",
			func(ctx ginkgo.SpecContext) {
				slashed := false
				for attempt := 1; attempt <= doubleProposalAttempts && !slashed; attempt++ {
					duty := slashingops.PlanNextEpoch(ctx, client, chain, func(epoch uint64) (beacon.ProposerDuty, bool) {
						return proposalDuty(ctx, client, epoch, candidates, scenario.duplicated)
					})
					val = rewards.Validator{Client: client, Chain: chain, Spec: rewardsSpec, Index: duty.ValidatorIndex}
					proposalSlot = duty.Slot
					slashed = scenario.run(ctx, val, duty.Slot) && slashingops.AwaitSlashed(ctx, val)
				}
				gomega.Expect(slashed).To(gomega.BeTrue(), "no double proposal was slashed in %d attempts", doubleProposalAttempts)

				ginkgo.By("finding the evidence a block included")
				slot, evidence := slashingops.FindEvidence(ctx, val, proposalSlot)
				first, second := slashingops.ExpectDoubleProposalEvidence(slot, evidence)
				gomega.Expect(first.Slot).To(gomega.Equal(proposalSlot), "the slot the duplicate proposed")
				ginkgo.GinkgoWriter.Printf("slot %d included a double proposal of slot %d with bodies %s and %s\n",
					slot, first.Slot, first.BodyRoot, second.BodyRoot)
				slashingSlot = slot
			}, ginkgo.SpecTimeout(doubleProposalTimeout))

		ginkgo.It("loses the slashing penalty in the block that includes the evidence, whose proposer earns the whistleblower reward",
			func(ctx ginkgo.SpecContext) {
				slashingops.ExpectSlashedInBlock(ctx, val, spec, slashingSlot, slashingops.DoubleProposal)
			}, ginkgo.SpecTimeout(time.Minute))

		// A failed attempt's duplicate can still be slashed later, so any
		// validator the suite duplicated may end up slashed.
		ginkgo.It("is slashed with no validator the suite did not duplicate, and the chain keeps finalizing", func(ctx ginkgo.SpecContext) {
			slashingops.ExpectOnlySlashed(ctx, client, scenario.duplicated)
			testsuite.ExpectFinalizing(ctx, client, chain)
		}, ginkgo.SpecTimeout(time.Minute))
	},
)
