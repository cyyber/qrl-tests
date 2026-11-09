//go:build e2e

package doubleproposal

import (
	"context"
	"net/netip"
	"strconv"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/slashingops"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// proposalDuty returns a proposal in epoch+1 that the duplicate can turn into
// a double proposal and nothing else. Besides what proposalCandidates checks,
// the proposer must not vote in the proposal's slot or the next: the two
// blocks split the nodes' heads until the next block, so a vote of the
// duplicate's then could differ from the primary's.
func proposalDuty(
	ctx context.Context, client *beacon.Client, epoch uint64, candidates, duplicated []uint64,
) (beacon.ProposerDuty, bool) {
	ginkgo.GinkgoHelper()
	next := epoch + 1
	current := testsuite.MustSucceed(client.ProposerDuties(ctx, epoch))
	upcoming := testsuite.MustSucceed(client.ProposerDuties(ctx, next))
	for _, duty := range proposalCandidates(current, upcoming, candidates, duplicated) {
		duties := testsuite.MustSucceed(client.AttesterDuties(ctx, next, []uint64{duty.ValidatorIndex}))
		gomega.Expect(duties).To(gomega.HaveLen(1), "attester duties of validator %d in epoch %d", duty.ValidatorIndex, next)
		if vote := duties[0].Slot; vote != duty.Slot && vote != duty.Slot+1 {
			return duty, true
		}
	}
	return beacon.ProposerDuty{}, false
}

// splitDuration is how long before and after the start of the proposal's slot
// the duplicate's node is cut off from the others. Setting up the split takes
// a helper container's run, about a second; the two blocks are built and
// published within a second of the slot's start.
const splitDuration = 2 * time.Second

// doubleProposal runs a second copy of a node-1 validator's key on the
// duplicate's node, across that key's proposal.
type doubleProposal struct {
	duplicate *slashingops.Node
	// peers are the addresses of the other nodes' beacon nodes, which the
	// duplicate's node is cut off from for the proposal.
	peers               []netip.Addr
	alltoolsImage       string
	withdrawalRecipient string
	// duplicated is the validators whose keys run has started a duplicate of.
	duplicated []uint64
}

// run runs the validator's key on the duplicate's node across the proposal of
// slot, which node 1's client also proposes, and reports whether the duplicate
// was up and its node cut off in time. Each block reveals the proposer's next
// RANDAO secret, so a node that has imported one copy's block rejects the
// other's: the duplicate's node is cut off from the others across the slot's
// start, and each side receives the other's block once it rejoins.
func (scenario *doubleProposal) run(ctx context.Context, val rewards.Validator, slot uint64) bool {
	ginkgo.GinkgoHelper()
	keystores := testsuite.MustSucceed(validatorops.GenesisKeystores(
		ctx, val.Client, scenario.alltoolsImage, scenario.withdrawalRecipient, val.Index))

	duplicate := scenario.duplicate
	ginkgo.By("starting a duplicate of validator " + strconv.FormatUint(val.Index, 10) +
		" for its proposal of slot " + strconv.FormatUint(slot, 10))
	scenario.duplicated = append(scenario.duplicated, val.Index)
	duplicate.StartClient(ctx, keystores)
	defer func() {
		gomega.Expect(duplicate.StopClient()).To(gomega.Succeed())
	}()
	start := val.Chain.SlotStartTime(slot)
	if time.Until(start) < splitDuration+time.Second {
		ginkgo.GinkgoWriter.Printf("the duplicate validator client started too late for slot %d\n", slot)
		return false
	}

	ginkgo.By("cutting the duplicate's node off from the others across slot " + strconv.FormatUint(slot, 10))
	gomega.Expect(testsuite.SleepUntil(ctx, start.Add(-splitDuration))).To(gomega.Succeed())
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
	gomega.Expect(testsuite.SleepUntil(ctx, start.Add(splitDuration))).To(gomega.Succeed())
	err := rejoin(context.WithoutCancel(ctx))
	rejoined = err == nil
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Stopping the duplicate once the slot is over keeps it from voting while
	// the two blocks split the heads.
	gomega.Expect(testsuite.SleepUntil(ctx, val.Chain.SlotStartTime(slot+1))).To(gomega.Succeed())
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
