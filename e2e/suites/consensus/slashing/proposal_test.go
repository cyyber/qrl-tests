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
	// doubleProposalAttempts is how many proposals the suite duplicates: a
	// duplicate that starts too late for its slot proposes nothing.
	doubleProposalAttempts = 3
	// doubleProposalTimeout covers every attempt: up to two epochs to plan
	// one, the epoch of the proposal, and the epochs the evidence takes to be
	// included.
	doubleProposalTimeout = 15 * time.Minute
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
			nodes := testsuite.MustSucceed(runtime.Nodes(ctx))
			gomega.Expect(nodes).To(gomega.HaveLen(3), "the suite proposes on two nodes, and a third watches")
			gomega.Expect(nodes[1].BeaconGRPC).NotTo(gomega.BeEmpty(), "beacon gRPC is not published")
			gomega.Expect(runtime.ValidatorImage).NotTo(gomega.BeEmpty(), "validator image is not configured")
			gomega.Expect(runtime.QrysmAlltoolsImage).NotTo(gomega.BeEmpty(), "Qrysm all-tools image is not configured")
			duplicate = &node{Node: nodes[1], graffiti: "duplicate-proposer"}
			scenario = doubleProposal{
				duplicate:           duplicate,
				peers:               []netip.Addr{beaconAddress(nodes[0]), beaconAddress(nodes[2])},
				alltoolsImage:       runtime.QrysmAlltoolsImage,
				withdrawalRecipient: nodes[0].Address.Hex(),
			}
			candidates = runtime.Profile.ParticipantValidators(0)
			gomega.Expect(candidates).NotTo(gomega.BeEmpty(), "profile %q runs no validators on node 1", runtime.Profile)

			client = nodes[0].Beacon
			chain = testsuite.MustSucceed(chaininfo.Load(ctx, client))
			rewardsSpec = testsuite.MustSucceed(rewards.LoadSpec(ctx, client))
			spec = testsuite.MustSucceed(loadSlashingSpec(ctx, client))
		}, ginkgo.NodeTimeout(time.Minute))

		ginkgo.AfterEach(func() {
			if duplicate != nil {
				testsuite.ReportLogsOnFailure(duplicate.graffiti+" validator client", duplicate.client)
			}
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(duplicate.stopClient()).To(gomega.Succeed())
		})

		ginkgo.It("is slashed for a double proposal once a second client runs its key on another node",
			func(ctx ginkgo.SpecContext) {
				for attempt := 1; ; attempt++ {
					gomega.Expect(attempt).To(gomega.BeNumerically("<=", doubleProposalAttempts),
						"no double proposal was slashed after %d duplicated proposals", doubleProposalAttempts)
					index, slot := planProposal(ctx, client, chain, candidates)
					val = rewards.Validator{Client: client, Chain: chain, Spec: rewardsSpec, Index: index}
					if scenario.run(ctx, val, slot) && awaitSlashed(ctx, val) {
						proposalSlot = slot
						break
					}
				}

				ginkgo.By("finding the evidence a block included")
				slot, evidence := findEvidence(ctx, val, proposalSlot)
				gomega.Expect(evidence.attester).To(gomega.BeNil(),
					"the block at slot %d slashed the validator for a double vote", slot)
				gomega.Expect(evidence.proposer).NotTo(gomega.BeNil())
				first, second := evidence.proposer.Header1, evidence.proposer.Header2
				gomega.Expect(isDoubleProposal(first, second)).To(gomega.BeTrue(),
					"the headers of slots %d and %d are not a double proposal", first.Slot, second.Slot)
				gomega.Expect(first.Slot).To(gomega.Equal(proposalSlot), "the slot the duplicate proposed")
				ginkgo.GinkgoWriter.Printf("slot %d included a double proposal of slot %d with bodies %s and %s\n",
					slot, first.Slot, first.BodyRoot, second.BodyRoot)
				slashingSlot = slot
			}, ginkgo.SpecTimeout(doubleProposalTimeout))

		ginkgo.It("pays the slashing penalty in the block that includes the evidence, and its proposer the whistleblower reward",
			func(ctx ginkgo.SpecContext) {
				expectSlashedInBlock(ctx, val, spec, slashingSlot, true)
			}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("slashes no validator the suite did not set out to, and the chain keeps finalizing", func(ctx ginkgo.SpecContext) {
			expectOnlySuiteSlashings(ctx, client, chain)
		}, ginkgo.SpecTimeout(time.Minute))
	},
)

// planProposal waits until the next epoch has a proposal that suits a double
// proposal, and returns its proposer and slot. It plans only during the first
// half of an epoch, so at least half an epoch is left to start the duplicate
// validator client, even for a proposal in the next epoch's first slot.
func planProposal(ctx context.Context, client *beacon.Client, chain chaininfo.Info, candidates []uint64) (uint64, uint64) {
	ginkgo.GinkgoHelper()
	for {
		now := time.Now()
		head := testsuite.MustSucceed(client.HeadSlot(ctx))
		epoch := max(chain.EpochAt(now), chain.Epoch(head))
		start := slotStart(chain, epoch*chain.SlotsPerEpoch)
		if now.Before(start.Add(chain.EpochDuration / 2)) {
			if index, slot, ok := proposalSlot(ctx, client, epoch, candidates); ok {
				return index, slot
			}
		}
		gomega.Expect(testsuite.SleepUntil(ctx, start.Add(chain.EpochDuration))).To(gomega.Succeed())
	}
}

// proposalSlot returns a proposal of the epoch after epoch that the duplicate
// can turn into a double proposal and nothing else. Besides what
// proposalCandidates checks, the proposer must not vote in the proposal's
// slot or the next: the two blocks split the nodes' heads until the next block,
// so a vote of the duplicate's then could differ from the primary's.
func proposalSlot(ctx context.Context, client *beacon.Client, epoch uint64, candidates []uint64) (uint64, uint64, bool) {
	ginkgo.GinkgoHelper()
	next := epoch + 1
	current := testsuite.MustSucceed(client.ProposerDuties(ctx, epoch))
	upcoming := testsuite.MustSucceed(client.ProposerDuties(ctx, next))
	for _, duty := range proposalCandidates(current, upcoming, candidates, suiteSlashed) {
		duties := testsuite.MustSucceed(client.AttesterDuties(ctx, next, []uint64{duty.ValidatorIndex}))
		gomega.Expect(duties).To(gomega.HaveLen(1), "attester duties of validator %d in epoch %d", duty.ValidatorIndex, next)
		if vote := duties[0].Slot; vote != duty.Slot && vote != duty.Slot+1 {
			return duty.ValidatorIndex, duty.Slot, true
		}
	}
	return 0, 0, false
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
