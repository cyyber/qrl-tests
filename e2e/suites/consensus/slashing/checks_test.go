//go:build e2e

package slashing

import (
	"context"
	"strconv"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// expectSlashedInBlock checks the block at slot that slashes the validator: it
// pays the initial penalty, sets the exit and withdrawable epochs, and pays its
// proposer the whistleblower reward for an attester slashing. It returns the
// validator's record at the block.
func expectSlashedInBlock(ctx context.Context, val rewards.Validator, spec slashingSpec, slot uint64) beacon.Validator {
	ginkgo.GinkgoHelper()
	index := strconv.FormatUint(val.Index, 10)
	before := testsuite.MustSucceed(val.Client.ValidatorAt(ctx, strconv.FormatUint(slot-1, 10), index))
	on := testsuite.MustSucceed(val.Client.ValidatorAt(ctx, strconv.FormatUint(slot, 10), index))
	gomega.Expect(before.Slashed).To(gomega.BeFalse())
	gomega.Expect(on.Slashed).To(gomega.BeTrue())
	gomega.Expect(on.Status).To(gomega.Equal("active_slashed"))

	// The block's own duties change the balance too, and a block at an
	// epoch's first slot also follows epoch processing, which applies the
	// attestation rewards of epoch-2.
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
		"balance at slot %d is %d, want %d: %d before, minus the %d shor penalty, plus %d shor from the block's duties and %d from epoch processing",
		slot, on.Balance, want, before.Balance, penalty, blocks, processed)

	exit := spec.exitEpoch(epoch)
	gomega.Expect(on.ExitEpoch).To(gomega.Equal(exit), "exit epoch of a validator slashed in epoch %d", epoch)
	gomega.Expect(on.WithdrawableEpoch).To(gomega.Equal(spec.withdrawableEpoch(epoch, exit)),
		"withdrawable epoch of a validator slashed in epoch %d", epoch)

	blockRewards := testsuite.MustSucceed(val.Client.BlockRewards(ctx, strconv.FormatUint(slot, 10)))
	gomega.Expect(blockRewards.AttesterSlashings).To(gomega.Equal(spec.whistleblowerReward(before.EffectiveBalance)),
		"what the block's proposer earned for the evidence")
	gomega.Expect(blockRewards.ProposerSlashings).To(gomega.BeZero())
	return on
}

// expectMissedVotePenalty checks that the validator's votes of epoch cost
// exactly the missed source and target penalties, and that its balance moved
// by that and its block duties alone.
func expectMissedVotePenalty(ctx context.Context, val rewards.Validator, epoch uint64) {
	ginkgo.GinkgoHelper()
	reward := testsuite.MustSucceed(val.AttestationReward(ctx, epoch))
	amounts := testsuite.MustSucceed(val.VoteAmounts(ctx, epoch))
	gomega.Expect(reward).To(gomega.Equal(beacon.AttestationReward{
		ValidatorIndex: val.Index,
		Source:         -int64(amounts.MissedSource),
		Target:         -int64(amounts.MissedTarget),
	}), "attestation rewards of epoch %d", epoch)

	before := testsuite.MustSucceed(val.BalanceAtEpochStart(ctx, epoch+1))
	after := testsuite.MustSucceed(val.BalanceAtEpochStart(ctx, epoch+2))
	blocks := testsuite.MustSucceed(val.BlockBalanceChanges(ctx, epoch+1))
	penalty := amounts.MissedSource + amounts.MissedTarget
	want := int64(before) - int64(penalty) + blocks
	gomega.Expect(int64(after)).To(gomega.Equal(want),
		"balance at the start of epoch %d is %d, want %d: %d before, minus the %d shor penalty of epoch %d, plus %d shor from blocks",
		epoch+2, after, want, before, penalty, epoch, blocks)
}
