//go:build e2e

package slashing

import (
	"context"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/deposit"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const (
	doubleProposalAttempts = 3
	doubleProposalTimeout  = 15 * time.Minute
)

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
			duplicate    *node
			scenario     doubleProposal
			client       *beacon.Client
			chain        chaininfo.Info
			rewardsSpec  rewards.Spec
			spec         slashingSpec
			candidates   []uint64
			val          rewards.Validator
			proposalSlot uint64
			slashingSlot uint64
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime := testsuite.LoadRuntime()
			nodes := loadNodes(ctx, runtime)
			gomega.Expect(nodes[duplicateParticipant].BeaconGRPC).NotTo(gomega.BeEmpty(), "beacon gRPC is not published")
			gomega.Expect(runtime.ValidatorImage).NotTo(gomega.BeEmpty(), "validator image is not configured")
			gomega.Expect(runtime.QrysmAlltoolsImage).NotTo(gomega.BeEmpty(), "Qrysm all-tools image is not configured")
			duplicate = &node{Node: nodes[duplicateParticipant], graffiti: "duplicate-proposer"}
			scenario = doubleProposal{
				duplicate:           duplicate,
				peers:               []netip.Addr{beaconAddress(nodes[primaryParticipant]), beaconAddress(nodes[watcherParticipant])},
				alltoolsImage:       runtime.QrysmAlltoolsImage,
				withdrawalRecipient: nodes[primaryParticipant].Address.Hex(),
			}
			candidates = runtime.Profile.ParticipantValidators(primaryParticipant)
			gomega.Expect(candidates).NotTo(gomega.BeEmpty(), "profile %q runs no validators on node 1", runtime.Profile)

			client = nodes[primaryParticipant].Beacon
			chain = testsuite.MustSucceed(chaininfo.Load(ctx, client))
			expectShortEpochs(chain)
			rewardsSpec = testsuite.MustSucceed(rewards.LoadSpec(ctx, client))
			spec = testsuite.MustSucceed(loadSlashingSpec(ctx, client))
		}, ginkgo.NodeTimeout(time.Minute))

		ginkgo.AfterAll(func() {
			gomega.Expect(duplicate.stopClient()).To(gomega.Succeed())
		})

		ginkgo.It("is slashed for a double proposal once a second client runs its key on another node",
			func(ctx ginkgo.SpecContext) {
				slashed := false
				for attempt := 1; attempt <= doubleProposalAttempts && !slashed; attempt++ {
					duty := planNextEpoch(ctx, client, chain, func(epoch uint64) (beacon.ProposerDuty, bool) {
						return proposalDuty(ctx, client, epoch, candidates)
					})
					val = rewards.Validator{Client: client, Chain: chain, Spec: rewardsSpec, Index: duty.ValidatorIndex}
					proposalSlot = duty.Slot
					slashed = scenario.run(ctx, val, duty.Slot) && awaitSlashed(ctx, val)
				}
				gomega.Expect(slashed).To(gomega.BeTrue(), "no double proposal was slashed in %d attempts", doubleProposalAttempts)

				ginkgo.By("finding the evidence a block included")
				slot, evidence := findEvidence(ctx, val, proposalSlot)
				first, second := expectDoubleProposalEvidence(slot, evidence)
				gomega.Expect(first.Slot).To(gomega.Equal(proposalSlot), "the slot the duplicate proposed")
				ginkgo.GinkgoWriter.Printf("slot %d included a double proposal of slot %d with bodies %s and %s\n",
					slot, first.Slot, first.BodyRoot, second.BodyRoot)
				slashingSlot = slot
			}, ginkgo.SpecTimeout(doubleProposalTimeout))

		ginkgo.It("loses the slashing penalty in the block that includes the evidence, whose proposer earns the whistleblower reward",
			func(ctx ginkgo.SpecContext) {
				expectSlashedInBlock(ctx, val, spec, slashingSlot, true)
			}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("is slashed with no validator the suite did not duplicate, and the chain keeps finalizing", func(ctx ginkgo.SpecContext) {
			expectOnlyDuplicatesSlashed(ctx, client, chain)
		}, ginkgo.SpecTimeout(time.Minute))
	},
)

// proposalDuty returns a proposal of the epoch after epoch that the duplicate
// can turn into a double proposal and nothing else. Besides what
// proposalCandidates checks, the proposer must not vote in the proposal's
// slot or the next: the two blocks split the nodes' heads until the next block,
// so a vote of the duplicate's then could differ from the primary's.
func proposalDuty(ctx context.Context, client *beacon.Client, epoch uint64, candidates []uint64) (beacon.ProposerDuty, bool) {
	ginkgo.GinkgoHelper()
	next := epoch + 1
	current := testsuite.MustSucceed(client.ProposerDuties(ctx, epoch))
	upcoming := testsuite.MustSucceed(client.ProposerDuties(ctx, next))
	for _, duty := range proposalCandidates(current, upcoming, candidates, suiteDuplicated) {
		duties := testsuite.MustSucceed(client.AttesterDuties(ctx, next, []uint64{duty.ValidatorIndex}))
		gomega.Expect(duties).To(gomega.HaveLen(1), "attester duties of validator %d in epoch %d", duty.ValidatorIndex, next)
		if vote := duties[0].Slot; vote != duty.Slot && vote != duty.Slot+1 {
			return duty, true
		}
	}
	return beacon.ProposerDuty{}, false
}

// splitSeconds is how long before and after the start of the proposal's slot
// the duplicate's node is cut off from the others. Setting up the split takes
// a helper container's run, about a second; the two blocks are built and
// published within a second of the slot's start.
const splitSeconds = 2

// doubleProposal runs a second copy of a node-1 validator's key on the
// duplicate's node, across that key's proposal.
type doubleProposal struct {
	duplicate *node
	// peers are the addresses of the other nodes' beacon nodes, which the
	// duplicate's node is cut off from for the proposal.
	peers               []netip.Addr
	alltoolsImage       string
	withdrawalRecipient string
}

// run runs the validator's key on the duplicate's node until the proposal of
// slot is done, while node 1's client proposes it too. It reports whether the
// duplicate was up and its node cut off in time for the proposal.
//
// A proposal's RANDAO reveal must be the pre-image of the proposer's
// commitment in the state it builds on, and each block moves the commitment
// one layer down the proposer's hash onion. A node that has imported the
// primary's block therefore refuses the duplicate's reveal, so the two copies
// both propose only if neither node sees the other's block in time: the
// duplicate's node is cut off from the others across the start of slot, as
// across a network split. Once it rejoins, each side receives the other's
// block.
func (scenario doubleProposal) run(ctx context.Context, val rewards.Validator, slot uint64) bool {
	ginkgo.GinkgoHelper()
	derived := testsuite.MustSucceed(deposit.GenesisKey(ctx, deposit.GenesisKeyConfig{
		Image:               scenario.alltoolsImage,
		Mnemonic:            devnet.GenesisMnemonic,
		Index:               val.Index,
		WithdrawalRecipient: scenario.withdrawalRecipient,
		Password:            validatorops.KeystorePassword,
	}))
	record := testsuite.MustSucceed(val.Client.Validator(ctx, strconv.FormatUint(val.Index, 10)))
	gomega.Expect(strings.EqualFold(record.PublicKey, derived.PublicKey)).To(gomega.BeTrue(),
		"genesis validator %d is not the key derived for its index", val.Index)

	duplicate := scenario.duplicate
	ginkgo.By("starting a duplicate of validator " + strconv.FormatUint(val.Index, 10) +
		" for its proposal of slot " + strconv.FormatUint(slot, 10))
	duplicating(val.Index)
	duplicate.startClient(ctx, derived.Keystores)
	defer func() {
		gomega.Expect(duplicate.stopClient()).To(gomega.Succeed())
	}()
	start := slotStart(val.Chain, slot)
	split := splitSeconds * time.Second
	if time.Until(start) < split+time.Second {
		ginkgo.GinkgoWriter.Printf("the duplicate validator client started too late for slot %d\n", slot)
		return false
	}

	ginkgo.By("cutting the duplicate's node off from the others across slot " + strconv.FormatUint(slot, 10))
	gomega.Expect(testsuite.SleepUntil(ctx, start.Add(-split))).To(gomega.Succeed())
	rejoin := testsuite.MustSucceed(devnet.IsolateService(ctx, duplicate.ConsensusServiceID, scenario.peers))
	rejoined := false
	defer func() {
		// Rejoin even when the spec has timed out, or the node stays cut off.
		if !rejoined {
			gomega.Expect(rejoin(context.WithoutCancel(ctx))).To(gomega.Succeed())
		}
	}()
	if !time.Now().Before(start) {
		ginkgo.GinkgoWriter.Printf("the duplicate's node was cut off too late for slot %d\n", slot)
		return false
	}
	gomega.Expect(testsuite.SleepUntil(ctx, start.Add(split))).To(gomega.Succeed())
	rejoined = true
	gomega.Expect(rejoin(context.WithoutCancel(ctx))).To(gomega.Succeed())

	// Stopping the duplicate once the slot is over keeps it from voting while
	// the two blocks split the heads.
	gomega.Expect(testsuite.SleepUntil(ctx, slotStart(val.Chain, slot+1))).To(gomega.Succeed())
	return true
}

// beaconAddress is the address a node's beacon node has on the devnet's
// network, where its peers reach it.
func beaconAddress(node *live.Node) netip.Addr {
	ginkgo.GinkgoHelper()
	address, err := netip.ParseAddr(node.Participant.Consensus.PrivateIP)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "private address of participant %d's beacon node", node.Participant.Index)
	return address
}
