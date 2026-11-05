//go:build e2e

package slashing

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const detectionEpochs = 4

// suiteSlashed is every validator the suite's scenarios have slashed. Ginkgo
// may run the scenarios in either order on the same network, so each checks
// that no validator outside this list is slashed.
var suiteSlashed []uint64

// awaitSlashed reports whether the validator is slashed within
// detectionEpochs, and records it in suiteSlashed if it is.
func awaitSlashed(ctx context.Context, val rewards.Validator) bool {
	ginkgo.GinkgoHelper()
	deadline := time.Now().Add(detectionEpochs * val.Chain.EpochDuration)
	index := strconv.FormatUint(val.Index, 10)
	for time.Now().Before(deadline) {
		record := testsuite.MustSucceed(val.Client.Validator(ctx, index))
		if record.Slashed {
			suiteSlashed = append(suiteSlashed, val.Index)
			return true
		}
		gomega.Expect(testsuite.SleepUntil(ctx, time.Now().Add(rewards.PollInterval))).To(gomega.Succeed())
	}
	ginkgo.GinkgoWriter.Printf("validator %d was not slashed within %d epochs\n", val.Index, detectionEpochs)
	return false
}

// findEvidence searches the blocks from the given slot up to the head, and
// returns the first one that slashes the validator, with its evidence.
func findEvidence(ctx context.Context, val rewards.Validator, from uint64) (uint64, slashingEvidence) {
	ginkgo.GinkgoHelper()
	head := testsuite.MustSucceed(val.Client.HeadSlot(ctx))
	for slot := from; slot <= head; slot++ {
		operations, err := val.Client.BlockOperations(ctx, strconv.FormatUint(slot, 10))
		if beacon.IsNotFound(err) {
			continue
		}
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		if evidence, found := evidenceAgainst(operations, val.Index); found {
			return slot, evidence
		}
	}
	ginkgo.Fail("no block from slot " + strconv.FormatUint(from, 10) + " slashes the validator")
	return 0, slashingEvidence{}
}

// expectSlashedInBlock checks the block at slot that slashes the validator:
// it pays the initial penalty, sets the exit and withdrawable epochs, and pays
// its proposer the whistleblower reward for the kind of evidence it includes.
// It returns the validator's record before and at the block.
func expectSlashedInBlock(
	ctx context.Context, val rewards.Validator, spec slashingSpec, slot uint64, doubleProposal bool,
) (before, on beacon.Validator) {
	ginkgo.GinkgoHelper()
	index := strconv.FormatUint(val.Index, 10)
	before = testsuite.MustSucceed(val.Client.ValidatorAt(ctx, strconv.FormatUint(slot-1, 10), index))
	on = testsuite.MustSucceed(val.Client.ValidatorAt(ctx, strconv.FormatUint(slot, 10), index))
	gomega.Expect(before.Slashed).To(gomega.BeFalse())
	gomega.Expect(on.Slashed).To(gomega.BeTrue())
	gomega.Expect(on.Status).To(gomega.Equal("active_slashed"))

	// The block's own duties change the balance too, and a block at an
	// epoch's first slot also follows epoch processing.
	epoch := val.Chain.Epoch(slot)
	penalty := spec.initialPenalty(before.EffectiveBalance)
	blocks := testsuite.MustSucceed(val.BlockBalanceChange(ctx, slot))
	var processed int64
	if slot%val.Chain.SlotsPerEpoch == 0 {
		reward := testsuite.MustSucceed(val.AttestationReward(ctx, epoch-2))
		processed = reward.Head + reward.Source + reward.Target
	}
	want := int64(before.Balance) - int64(penalty) + blocks + processed
	gomega.Expect(int64(on.Balance)).To(gomega.Equal(want),
		"balance went from %d to %d at slot %d: the %d shor penalty, %d shor from the block and %d from epoch processing make %d",
		before.Balance, on.Balance, slot, penalty, blocks, processed, want)

	exit := spec.exitEpoch(epoch)
	gomega.Expect(on.ExitEpoch).To(gomega.Equal(exit), "exit epoch of a validator slashed in epoch %d", epoch)
	gomega.Expect(on.WithdrawableEpoch).To(gomega.Equal(spec.withdrawableEpoch(epoch, exit)),
		"withdrawable epoch of a validator slashed in epoch %d", epoch)

	blockRewards := testsuite.MustSucceed(val.Client.BlockRewards(ctx, strconv.FormatUint(slot, 10)))
	reward := spec.whistleblowerReward(before.EffectiveBalance)
	wantProposerSlashings, wantAttesterSlashings := uint64(0), reward
	if doubleProposal {
		wantProposerSlashings, wantAttesterSlashings = reward, 0
	}
	gomega.Expect(blockRewards.ProposerSlashings).To(gomega.Equal(wantProposerSlashings),
		"what the block's proposer earned for proposer slashings")
	gomega.Expect(blockRewards.AttesterSlashings).To(gomega.Equal(wantAttesterSlashings),
		"what the block's proposer earned for attester slashings")
	return before, on
}

// expectOnlySuiteSlashings checks that the chain has slashed exactly the
// validators in suiteSlashed, and that it keeps finalizing.
func expectOnlySuiteSlashings(ctx context.Context, client *beacon.Client, chain chaininfo.Info) {
	ginkgo.GinkgoHelper()
	slashed := testsuite.MustSucceed(client.SlashedValidators(ctx))
	indices := make([]uint64, len(slashed))
	for position, record := range slashed {
		indices[position] = record.Index
	}
	slices.Sort(indices)
	gomega.Expect(indices).To(gomega.Equal(slices.Sorted(slices.Values(suiteSlashed))))

	// A healthy chain finalizes the epoch before the previous one.
	head := testsuite.MustSucceed(client.HeadSlot(ctx))
	checkpoints := testsuite.MustSucceed(client.FinalityCheckpoints(ctx))
	gomega.Expect(checkpoints.Finalized.Epoch+3).To(gomega.BeNumerically(">=", chain.Epoch(head)),
		"finalized epoch %d at head slot %d", checkpoints.Finalized.Epoch, head)
}

func slotStart(chain chaininfo.Info, slot uint64) time.Time {
	return chain.GenesisTime.Add(time.Duration(slot) * chain.SlotDuration)
}
