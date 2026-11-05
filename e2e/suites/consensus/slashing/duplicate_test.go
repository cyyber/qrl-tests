//go:build e2e

package slashing

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// detectionEpochs is how long the suite gives the slashers and the proposers
// after a double vote: a slasher reports it within a slot or two of seeing
// both votes, and the next block whose proposer has the report includes it.
const detectionEpochs = 4

// doubleVote runs the validator's key on the duplicate's node too, for one
// vote that the two nodes see differently. On a healthy devnet both nodes
// have the same head when a vote is due, so two clients sign the same vote,
// which is no offense. A node that sees a block late, as one on a slow link
// does on a large network, gives its client the block before as the head:
// the duplicate then signs a second vote for the same target, a double vote.
type doubleVote struct {
	val                  rewards.Validator
	primary, duplicate   *node
	pausedNodeValidators []uint64
	keystores            []sidecar.File
}

// attempt starts the duplicate for a vote that the primary's node will see
// the block of, delays that block to the duplicate's node, and stops the
// duplicate once it has voted. It returns the vote's slot and whether the
// duplicate was up in time to vote.
func (scenario *doubleVote) attempt(ctx context.Context) (uint64, bool) {
	ginkgo.GinkgoHelper()
	slot := scenario.awaitVoteSlot(ctx)
	ginkgo.By("starting the duplicate validator client for the vote of slot " + strconv.FormatUint(slot, 10))
	scenario.duplicate.startClient(ctx, scenario.keystores)
	defer func() {
		gomega.Expect(scenario.duplicate.stopClient()).To(gomega.Succeed())
	}()
	if time.Until(scenario.slotStart(slot)) < scenario.val.Chain.SlotDuration {
		ginkgo.GinkgoWriter.Printf("the duplicate validator client started too late for slot %d\n", slot)
		return slot, false
	}

	ginkgo.By("delaying the block of slot " + strconv.FormatUint(slot, 10) + " to the duplicate's beacon node")
	scenario.delayBlock(ctx, slot)
	// The duplicate publishes its vote once its node answers. Stopping it
	// within the next slot keeps it from proposing: awaitVoteSlot picked a
	// vote before the last slot of an epoch in which the key proposes none.
	gomega.Expect(sleepUntil(ctx, scenario.slotStart(slot+1).Add(scenario.val.Chain.SlotDuration/2))).To(gomega.Succeed())
	return slot, true
}

// awaitVoteSlot waits for an epoch whose next epoch suits a double vote, and
// returns the slot of the validator's vote in that next epoch. Starting the
// duplicate takes a while, so only the first half of an epoch plans the next.
func (scenario *doubleVote) awaitVoteSlot(ctx context.Context) uint64 {
	ginkgo.GinkgoHelper()
	chain := scenario.val.Chain
	for {
		now := time.Now()
		head := testsuite.MustSucceed(scenario.val.Client.HeadSlot(ctx))
		epoch := max(chain.EpochAt(now), chain.Epoch(head))
		start := scenario.slotStart(epoch * chain.SlotsPerEpoch)
		if now.Before(start.Add(chain.EpochDuration / 2)) {
			if slot, ok := scenario.voteSlot(ctx, epoch); ok {
				return slot
			}
		}
		gomega.Expect(sleepUntil(ctx, start.Add(chain.EpochDuration))).To(gomega.Succeed())
	}
}

// voteSlot returns the slot of the validator's vote in the epoch after epoch
// if a double vote there is the only offense the duplicate can commit: the
// key proposes in neither epoch, as a duplicate's proposal is a double
// proposal, the vote is not the epoch's last slot, and a validator of the
// primary's node proposes the vote's slot, so the delayed node is the only
// one that sees that block late.
func (scenario *doubleVote) voteSlot(ctx context.Context, epoch uint64) (uint64, bool) {
	ginkgo.GinkgoHelper()
	client := scenario.val.Client
	next := epoch + 1
	upcoming := testsuite.MustSucceed(client.ProposerDuties(ctx, next))
	proposals := slices.Concat(testsuite.MustSucceed(client.ProposerDuties(ctx, epoch)), upcoming)
	if slices.ContainsFunc(proposals, func(duty beacon.ProposerDuty) bool {
		return duty.ValidatorIndex == scenario.val.Index
	}) {
		return 0, false
	}

	duties := testsuite.MustSucceed(client.AttesterDuties(ctx, next, []uint64{scenario.val.Index}))
	gomega.Expect(duties).To(gomega.HaveLen(1), "attester duties of epoch %d", next)
	slot := duties[0].Slot
	if slot%scenario.val.Chain.SlotsPerEpoch == scenario.val.Chain.SlotsPerEpoch-1 {
		return 0, false
	}
	proposer := slices.IndexFunc(upcoming, func(duty beacon.ProposerDuty) bool { return duty.Slot == slot })
	if proposer < 0 || slices.Contains(scenario.pausedNodeValidators, upcoming[proposer].ValidatorIndex) ||
		upcoming[proposer].ValidatorIndex == scenario.val.Index {
		return 0, false
	}
	return slot, true
}

// delayBlock pauses the duplicate's beacon node from just before slot until
// after its client asks for the vote, a third into the slot. The node answers
// the waiting request once it resumes, before it has processed the slot's
// block that waits in its sockets, so its head is still the block before.
func (scenario *doubleVote) delayBlock(ctx context.Context, slot uint64) {
	ginkgo.GinkgoHelper()
	start := scenario.slotStart(slot)
	gomega.Expect(sleepUntil(ctx, start.Add(-time.Second))).To(gomega.Succeed())
	resume := testsuite.MustSucceed(devnet.PauseService(ctx, scenario.duplicate.ConsensusServiceID))
	defer func() {
		// Resume even when the spec has timed out, or the node stays frozen.
		gomega.Expect(resume(context.WithoutCancel(ctx))).To(gomega.Succeed())
	}()
	gomega.Expect(sleepUntil(ctx, start.Add(scenario.val.Chain.SlotDuration/3+time.Second))).To(gomega.Succeed())
}

// awaitSlashed reports whether the validator is slashed within
// detectionEpochs.
func (scenario *doubleVote) awaitSlashed(ctx context.Context) bool {
	ginkgo.GinkgoHelper()
	deadline := time.Now().Add(detectionEpochs * scenario.val.Chain.EpochDuration)
	index := strconv.FormatUint(scenario.val.Index, 10)
	for time.Now().Before(deadline) {
		record := testsuite.MustSucceed(scenario.val.Client.Validator(ctx, index))
		if record.Slashed {
			return true
		}
		gomega.Expect(sleepUntil(ctx, time.Now().Add(rewards.PollInterval))).To(gomega.Succeed())
	}
	ginkgo.GinkgoWriter.Printf("validator %d was not slashed within %d epochs\n", scenario.val.Index, detectionEpochs)
	return false
}

// findEvidence returns the first block from slot from on that slashes the
// validator, and its evidence.
func (scenario *doubleVote) findEvidence(ctx context.Context, from uint64) (uint64, slashingEvidence) {
	ginkgo.GinkgoHelper()
	client := scenario.val.Client
	head := testsuite.MustSucceed(client.HeadSlot(ctx))
	for slot := from; slot <= head; slot++ {
		operations, err := client.BlockOperations(ctx, strconv.FormatUint(slot, 10))
		if beacon.IsNotFound(err) {
			continue
		}
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		if evidence, found := evidenceAgainst(operations, scenario.val.Index); found {
			return slot, evidence
		}
	}
	ginkgo.Fail("no block from slot " + strconv.FormatUint(from, 10) + " slashes the validator")
	return 0, slashingEvidence{}
}

func (scenario *doubleVote) slotStart(slot uint64) time.Time {
	return scenario.val.Chain.GenesisTime.Add(time.Duration(slot) * scenario.val.Chain.SlotDuration)
}

func sleepUntil(ctx context.Context, deadline time.Time) error {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
