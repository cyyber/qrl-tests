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

const detectionEpochs = 4

// doubleVote runs a second copy of the validator's key on the duplicate's node
// for one vote. Healthy nodes share a head, so both copies would sign the same
// vote; delaying a block to the duplicate's node makes its vote differ.
type doubleVote struct {
	val                  rewards.Validator
	duplicate            *node
	pausedNodeValidators []uint64
	keystores            []sidecar.File
}

// attempt makes one try at a double vote. It starts the duplicate validator
// client, pauses that client's beacon node so the vote slot's block reaches
// it late, and then stops the client. It returns the vote slot and whether
// the client started in time.
func (scenario *doubleVote) attempt(ctx context.Context) (uint64, bool) {
	ginkgo.GinkgoHelper()
	slot := scenario.planVoteSlot(ctx)
	ginkgo.By("starting the duplicate validator client for the vote of slot " + strconv.FormatUint(slot, 10))
	scenario.duplicate.startClient(ctx, scenario.keystores)
	defer func() {
		gomega.Expect(scenario.duplicate.stopClient()).To(gomega.Succeed())
	}()
	if time.Until(scenario.slotStart(slot)) < scenario.val.Chain.SlotDuration {
		ginkgo.GinkgoWriter.Printf("the duplicate validator client started too late for slot %d\n", slot)
		return slot, false
	}

	ginkgo.By("pausing the duplicate's beacon node so it sees the block of slot " + strconv.FormatUint(slot, 10) + " late")
	scenario.pauseAcrossVote(ctx, slot)
	// Give the duplicate time to publish its vote before it is stopped.
	gomega.Expect(testsuite.SleepUntil(ctx, scenario.slotStart(slot+1).Add(scenario.val.Chain.SlotDuration/2))).To(gomega.Succeed())
	return slot, true
}

// planVoteSlot waits until the next epoch suits a double vote, and returns
// the validator's vote slot in it. It plans only during the first half of an
// epoch, so at least half an epoch is left to start the duplicate validator
// client, even for a vote in the next epoch's first slot.
func (scenario *doubleVote) planVoteSlot(ctx context.Context) uint64 {
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
		gomega.Expect(testsuite.SleepUntil(ctx, start.Add(chain.EpochDuration))).To(gomega.Succeed())
	}
}

// voteSlot returns the validator's vote slot in the epoch after epoch, if a
// double vote is the only offense the duplicate can commit there: the key
// proposes in neither epoch, a node-1 validator proposes the vote's slot, and
// the vote is not the epoch's last slot, so the duplicate is gone before an
// epoch whose proposals aren't checked.
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
	if proposer < 0 || slices.Contains(scenario.pausedNodeValidators, upcoming[proposer].ValidatorIndex) {
		return 0, false
	}
	return slot, true
}

// pauseAcrossVote pauses the duplicate's beacon node from just before slot
// until just after a third into it. On resuming, the node answers the vote
// request before it imports the slot's block, so the duplicate votes for the
// block before.
func (scenario *doubleVote) pauseAcrossVote(ctx context.Context, slot uint64) {
	ginkgo.GinkgoHelper()
	start := scenario.slotStart(slot)
	gomega.Expect(testsuite.SleepUntil(ctx, start.Add(-time.Second))).To(gomega.Succeed())
	resume := testsuite.MustSucceed(devnet.PauseService(ctx, scenario.duplicate.ConsensusServiceID))
	defer func() {
		// Resume even when the spec has timed out, or the node stays frozen.
		gomega.Expect(resume(context.WithoutCancel(ctx))).To(gomega.Succeed())
	}()
	gomega.Expect(testsuite.SleepUntil(ctx, start.Add(scenario.val.Chain.SlotDuration/3+time.Second))).To(gomega.Succeed())
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
		gomega.Expect(testsuite.SleepUntil(ctx, time.Now().Add(rewards.PollInterval))).To(gomega.Succeed())
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
