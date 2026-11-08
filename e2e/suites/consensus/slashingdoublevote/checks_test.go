//go:build e2e

package slashingdoublevote

import (
	"context"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// expectExitedSlashed checks the validator's record at the start of epoch: it
// has exited as slashed, and the withdrawable epoch set when it was slashed is
// still ahead.
func expectExitedSlashed(ctx context.Context, val rewards.Validator, epoch uint64, onSlashing beacon.Validator) {
	ginkgo.GinkgoHelper()
	record := testsuite.MustSucceed(val.RecordAtEpochStart(ctx, epoch))
	gomega.Expect(record.Status).To(gomega.Equal("exited_slashed"), "status at the start of epoch %d", epoch)
	gomega.Expect(record.WithdrawableEpoch).To(gomega.Equal(onSlashing.WithdrawableEpoch),
		"withdrawable epoch at the start of epoch %d", epoch)
	gomega.Expect(epoch).To(gomega.BeNumerically("<", record.WithdrawableEpoch),
		"the validator can withdraw at the start of epoch %d", epoch)
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
	blocksChange := testsuite.MustSucceed(val.BlockBalanceChanges(ctx, epoch+1))
	penalty := amounts.MissedSource + amounts.MissedTarget
	want := int64(before) - int64(penalty) + blocksChange
	gomega.Expect(int64(after)).To(gomega.Equal(want),
		"balance at the start of epoch %d is %d, want %d: %d before, minus the %d shor penalty of epoch %d, plus %d shor from blocks",
		epoch+2, after, want, before, penalty, epoch, blocksChange)
}
