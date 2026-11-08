//go:build e2e

package slashingdoublevote

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/e2e/internal/slashingops"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// doubleVote runs a second copy of the validator's key on the duplicate's node
// for one vote. Healthy nodes share a head, so both copies would sign the same
// vote; delaying a block to the duplicate's node makes its vote differ.
type doubleVote struct {
	val                  rewards.Validator
	duplicate            *slashingops.Node
	pausedNodeValidators []uint64
	keystores            []sidecar.File
}

// attempt makes one try at a double vote. It starts the duplicate validator
// client, pauses that client's beacon node so the vote slot's block reaches
// it late, and then stops the client. It returns the vote slot and whether
// the client started in time.
func (scenario *doubleVote) attempt(ctx context.Context) (uint64, bool) {
	ginkgo.GinkgoHelper()
	slot := slashingops.PlanNextEpoch(ctx, scenario.val.Client, scenario.val.Chain, func(epoch uint64) (uint64, bool) {
		return scenario.voteSlot(ctx, epoch)
	})

	ginkgo.By("starting the duplicate validator client for the vote of slot " + strconv.FormatUint(slot, 10))
	scenario.duplicate.StartClient(ctx, scenario.keystores)
	defer func() {
		gomega.Expect(scenario.duplicate.StopClient()).To(gomega.Succeed())
	}()
	if time.Until(slashingops.SlotStart(scenario.val.Chain, slot)) < scenario.val.Chain.SlotDuration {
		ginkgo.GinkgoWriter.Printf("the duplicate validator client started too late for slot %d\n", slot)
		return slot, false
	}

	ginkgo.By("pausing the duplicate's beacon node so it sees the block of slot " + strconv.FormatUint(slot, 10) + " late")
	scenario.pauseAcrossVote(ctx, slot)

	// Give the duplicate time to publish its vote before it is stopped.
	gomega.Expect(testsuite.SleepUntil(ctx, slashingops.SlotStart(scenario.val.Chain, slot+1).Add(scenario.val.Chain.SlotDuration/2))).To(gomega.Succeed())
	return slot, true
}

// voteSlot returns the validator's vote slot in epoch+1, if a double vote is
// the only offense the duplicate can commit there, and it does commit one:
//   - the key proposes in neither epoch nor epoch+1, so the duplicate never
//     double proposes; a validator is slashed only once, so that would keep
//     the double vote from being included;
//   - a node-1 validator proposes the vote's slot, so there is a block that
//     node 1 sees in time and the paused node 2 sees late;
//   - the vote is not the epoch's last slot, so the duplicate is stopped
//     before epoch+2, whose proposals aren't checked.
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
// request before it imports the slot's block, so the duplicate validator
// client votes for the previous block as the head.
func (scenario *doubleVote) pauseAcrossVote(ctx context.Context, slot uint64) {
	ginkgo.GinkgoHelper()
	start := slashingops.SlotStart(scenario.val.Chain, slot)
	gomega.Expect(testsuite.SleepUntil(ctx, start.Add(-time.Second))).To(gomega.Succeed())
	resume := testsuite.MustSucceed(devnet.PauseService(ctx, scenario.duplicate.ConsensusServiceID))
	defer func() {
		// Resume even when the spec has timed out, or the node stays frozen.
		gomega.Expect(resume(context.WithoutCancel(ctx))).To(gomega.Succeed())
	}()
	gomega.Expect(testsuite.SleepUntil(ctx, start.Add(scenario.val.Chain.SlotDuration/3+time.Second))).To(gomega.Succeed())
}
