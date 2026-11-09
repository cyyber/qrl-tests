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
	watcherParticipant = 2

	doubleProposalAttempts = 3
	doubleProposalTimeout  = 15 * time.Minute
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Double-proposal slashing E2E suite")
}

var _ = ginkgo.Describe(
	"A validator whose key a second client runs while it proposes",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "consensus", "slashing"),
	func() {
		var (
			// The primary is the validator client node 1's participant runs.
			// duplicate runs a second copy of one of its keys on node 2 across
			// that key's proposal, with a graffiti of its own, so the two
			// blocks differ, while node 2 is cut off from the other nodes so
			// that neither sees the other's block in time. The third node runs
			// no validators: it receives both blocks once the split ends, and
			// its slasher reports them.
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
			gomega.Expect(nodes).To(gomega.HaveLen(watcherParticipant+1), "the suite proposes on two nodes, and a third watches")
			gomega.Expect(nodes[slashingops.DuplicateParticipant].BeaconGRPC).NotTo(gomega.BeEmpty(), "beacon gRPC is not published")
			gomega.Expect(runtime.ValidatorImage).NotTo(gomega.BeEmpty(), "validator image is not configured")
			gomega.Expect(runtime.QrysmAlltoolsImage).NotTo(gomega.BeEmpty(), "Qrysm all-tools image is not configured")
			duplicate = &slashingops.Node{Node: nodes[slashingops.DuplicateParticipant], Graffiti: "duplicate-proposer"}
			scenario = &doubleProposal{
				duplicate: duplicate,
				peers: []netip.Addr{
					beaconAddress(nodes[slashingops.PrimaryParticipant]), beaconAddress(nodes[watcherParticipant]),
				},
				alltoolsImage:       runtime.QrysmAlltoolsImage,
				withdrawalRecipient: nodes[slashingops.PrimaryParticipant].Address.Hex(),
			}
			candidates = runtime.Profile.ParticipantValidators(slashingops.PrimaryParticipant)
			gomega.Expect(candidates).NotTo(gomega.BeEmpty(), "profile %q runs no validators on node 1", runtime.Profile)

			client = nodes[slashingops.PrimaryParticipant].Beacon
			chain = testsuite.MustSucceed(chaininfo.Load(ctx, client))
			slashingops.ExpectShortEpochs(chain)
			rewardsSpec = testsuite.MustSucceed(rewards.LoadSpec(ctx, client))
			spec = testsuite.MustSucceed(slashingops.LoadSpec(ctx, client))
			gomega.Expect(devnet.PrepareIsolation(ctx)).To(gomega.Succeed())
		}, ginkgo.NodeTimeout(3*time.Minute))

		ginkgo.AfterAll(func() {
			gomega.Expect(duplicate.StopClient()).To(gomega.Succeed())
		})

		ginkgo.It("is slashed for a double proposal once a second client runs its key on another node",
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
			slashingops.ExpectFinalizing(ctx, client, chain)
		}, ginkgo.SpecTimeout(time.Minute))
	},
)
