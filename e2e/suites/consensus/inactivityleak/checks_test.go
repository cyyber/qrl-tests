//go:build e2e

package inactivityleak

import (
	"context"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// expectMissedEpoch checks an epoch in which the validator cast no vote, its
// inactivity score having been score before the epoch was processed. The
// score rises by the bias, and drops back outside a leak. The rewards API
// reports the missed-vote penalties, with the inactivity penalty at the old
// score in the target's (cyyber/qrysm#113); the balance loses the inactivity
// penalty at the new one. It returns the epoch's amounts.
func expectMissedEpoch(ctx context.Context, val rewards.Validator, epoch, score uint64) rewards.LeakAmounts {
	ginkgo.GinkgoHelper()
	amounts := testsuite.MustSucceed(val.LeakVoteAmounts(ctx, epoch))
	expectScores(val, epoch, amounts, score, false)

	reward := testsuite.MustSucceed(val.AttestationReward(ctx, epoch))
	gomega.Expect(reward).To(gomega.Equal(beacon.AttestationReward{
		ValidatorIndex: val.Index,
		Source:         -int64(amounts.MissedSource),
		Target:         -int64(amounts.MissedTarget + amounts.ReportedInactivity),
	}), "attestation rewards of epoch %d: the target's is the %d shor missed-target penalty and the %d shor inactivity penalty at score %d",
		epoch, amounts.MissedTarget, amounts.ReportedInactivity, amounts.ScoreBefore)

	// The rewards of epoch apply when epoch+2 starts. The blocks of epoch+1
	// add the missed sync committee duties; the offline validator proposes
	// none.
	before := testsuite.MustSucceed(val.BalanceAtEpochStart(ctx, epoch+1))
	after := testsuite.MustSucceed(val.BalanceAtEpochStart(ctx, epoch+2))
	blocks := testsuite.MustSucceed(val.BlockBalanceChanges(ctx, epoch+1))
	missed := amounts.MissedSource + amounts.MissedTarget
	want := int64(before) - int64(missed+amounts.Inactivity) + blocks
	gomega.Expect(int64(after)).To(gomega.Equal(want),
		"balance went from %d to %d over epoch %d: the %d shor missed-vote and %d shor inactivity penalties of epoch %d, at score %d, and %d shor from blocks make %d",
		before, after, epoch+1, missed, amounts.Inactivity, epoch, amounts.ScoreAfter, blocks, want)
	return amounts
}

// expectUnpaidVotes checks an epoch processed in a leak in which the
// validator voted: timely votes earn nothing, a late source vote still costs
// its penalty, and the score stays zero.
func expectUnpaidVotes(ctx context.Context, val rewards.Validator, epoch uint64) {
	ginkgo.GinkgoHelper()
	amounts := testsuite.MustSucceed(val.LeakVoteAmounts(ctx, epoch))
	gomega.Expect(amounts.Leak).To(gomega.BeTrue(), "epoch %d is not processed in a leak", epoch)
	gomega.Expect(amounts.ReportedLeak).To(gomega.BeTrue(), "the rewards API does not count epoch %d as in a leak", epoch)
	gomega.Expect(amounts.Timely).To(gomega.Equal(beacon.IdealAttestationReward{EffectiveBalance: amounts.Timely.EffectiveBalance}),
		"what timely votes earn in epoch %d", epoch)
	expectScores(val, epoch, amounts, 0, true)

	// A healthy client's target vote is always timely. Whether its source
	// vote was depends on when a block included it, which the chain records.
	timely := testsuite.MustSucceed(val.TimelyVotes(ctx, epoch))
	want := beacon.AttestationReward{ValidatorIndex: val.Index}
	if !timely.Source {
		want.Source = -int64(amounts.MissedSource)
	}
	reward := testsuite.MustSucceed(val.AttestationReward(ctx, epoch))
	gomega.Expect(reward).To(gomega.Equal(want), "attestation rewards of epoch %d, whose votes were %+v", epoch, timely)

	// The blocks of epoch+1 add the validator's proposer and sync committee
	// rewards, which the leak leaves alone, and take any withdrawal.
	before := testsuite.MustSucceed(val.BalanceAtEpochStart(ctx, epoch+1))
	after := testsuite.MustSucceed(val.BalanceAtEpochStart(ctx, epoch+2))
	blocks := testsuite.MustSucceed(val.BlockBalanceChanges(ctx, epoch+1))
	wantBalance := int64(before) + reward.Source + blocks
	gomega.Expect(int64(after)).To(gomega.Equal(wantBalance),
		"balance went from %d to %d over epoch %d: the %d shor source figure of epoch %d and %d shor from blocks make %d",
		before, after, epoch+1, reward.Source, epoch, blocks, wantBalance)
}

// expectScores checks the validator's inactivity score before and after the
// processing of epoch: score before, and the rule's next score after.
func expectScores(val rewards.Validator, epoch uint64, amounts rewards.LeakAmounts, score uint64, timely bool) {
	ginkgo.GinkgoHelper()
	gomega.Expect(amounts.ScoreBefore).To(gomega.Equal(score),
		"inactivity score of validator %d before the processing of epoch %d", val.Index, epoch)
	want := val.Spec.NextInactivityScore(score, timely, amounts.Leak)
	gomega.Expect(amounts.ScoreAfter).To(gomega.Equal(want),
		"inactivity score of validator %d after the processing of epoch %d: from %d, a target vote that was timely: %t, in a leak: %t",
		val.Index, epoch, score, timely, amounts.Leak)
}
