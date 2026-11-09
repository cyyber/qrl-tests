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

// proposalDuty returns a candidate proposal in epoch+1 whose proposer doesn't
// vote in its slot or the next, while the two blocks split the nodes' heads.
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
		if voteSlot := duties[0].Slot; voteSlot != duty.Slot && voteSlot != duty.Slot+1 {
			return duty, true
		}
	}
	return beacon.ProposerDuty{}, false
}

const splitDuration = 2 * time.Second

// doubleProposal runs a second copy of a node-1 validator's key on the
// duplicate's node, across that key's proposal.
type doubleProposal struct {
	duplicate *slashingops.Node
	// peers are the beacon nodes the duplicate's node is cut off from.
	peers               []netip.Addr
	alltoolsImage       string
	withdrawalRecipient string
	// duplicated is the validators run has duplicated.
	duplicated []uint64
}

// run duplicates the validator's key on node 2 for its proposal of slot, and
// reports whether the duplicate was up and node 2 cut off in time. Node 2 is
// cut off across the slot's start because a node that has imported one copy's
// block rejects the other's RANDAO reveal.
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
