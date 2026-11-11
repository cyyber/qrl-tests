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

// expectMissedEpoch checks the inactivity scores, rewards and balance change
// of an epoch the validator missed, starting at score.
func expectMissedEpoch(ctx context.Context, val rewards.Validator, epoch, score uint64) rewards.LeakAmounts {
	ginkgo.GinkgoHelper()
	amounts := testsuite.MustSucceed(val.LeakVoteAmounts(ctx, epoch))
	expectScores(val, epoch, amounts, score, false)

	reward := testsuite.MustSucceed(val.AttestationReward(ctx, epoch))
	gomega.Expect(reward).To(gomega.Equal(beacon.AttestationReward{
		ValidatorIndex: val.Index,
		Source:         -int64(amounts.MissedSource),
		Target:         -int64(amounts.MissedTarget + amounts.InactivityPenalty),
	}), "attestation rewards of epoch %d: the target figure is the %d shor missed-target penalty plus the %d shor inactivity penalty at score %d",
		epoch, amounts.MissedTarget, amounts.InactivityPenalty, amounts.ScoreAfter)
	expectBalanceChange(ctx, val, epoch, reward)
	return amounts
}

// expectUnpaidVotes checks an epoch processed in a leak in which the
// validator voted: timely votes earn nothing, a late source vote still costs
// its penalty, and the score stays zero.
func expectUnpaidVotes(ctx context.Context, val rewards.Validator, epoch uint64) {
	ginkgo.GinkgoHelper()
	amounts := testsuite.MustSucceed(val.LeakVoteAmounts(ctx, epoch))
	gomega.Expect(amounts.Leak).To(gomega.BeTrue(), "epoch %d is not processed in a leak", epoch)
	gomega.Expect(amounts.Timely).To(gomega.Equal(beacon.IdealAttestationReward{EffectiveBalance: amounts.Timely.EffectiveBalance}),
		"what timely votes earn in epoch %d", epoch)
	expectScores(val, epoch, amounts, 0, true)

	// The target vote is always timely; the source vote depends on inclusion.
	timely := testsuite.MustSucceed(val.TimelyVotes(ctx, epoch))
	want := beacon.AttestationReward{ValidatorIndex: val.Index}
	if !timely.Source {
		want.Source = -int64(amounts.MissedSource)
	}
	reward := testsuite.MustSucceed(val.AttestationReward(ctx, epoch))
	gomega.Expect(reward).To(gomega.Equal(want), "attestation rewards of epoch %d, whose votes were %+v", epoch, timely)
	expectBalanceChange(ctx, val, epoch, reward)
}

// expectScores checks the validator's inactivity score before and after the
// processing of epoch.
func expectScores(val rewards.Validator, epoch uint64, amounts rewards.LeakAmounts, score uint64, timelyTarget bool) {
	ginkgo.GinkgoHelper()
	gomega.Expect(amounts.ScoreBefore).To(gomega.Equal(score),
		"inactivity score of validator %d before the processing of epoch %d", val.Index, epoch)
	want := val.Spec.NextInactivityScore(score, timelyTarget, amounts.Leak)
	gomega.Expect(amounts.ScoreAfter).To(gomega.Equal(want),
		"inactivity score of validator %d after the processing of epoch %d: from %d, a target vote that was timely: %t, in a leak: %t",
		val.Index, epoch, score, timelyTarget, amounts.Leak)
}

// expectBalanceChange checks that the balance changed over epoch+1 by the
// attestation reward of epoch plus what the blocks of epoch+1 paid or took.
func expectBalanceChange(ctx context.Context, val rewards.Validator, epoch uint64, reward beacon.AttestationReward) {
	ginkgo.GinkgoHelper()
	before := testsuite.MustSucceed(val.BalanceAtEpochStart(ctx, epoch+1))
	after := testsuite.MustSucceed(val.BalanceAtEpochStart(ctx, epoch+2))
	blocks := testsuite.MustSucceed(val.BlockBalanceChanges(ctx, epoch+1))
	attestation := reward.Head + reward.Source + reward.Target
	want := int64(before) + attestation + blocks
	gomega.Expect(int64(after)).To(gomega.Equal(want),
		"balance went from %d to %d over epoch %d: the %d shor attestation figure of epoch %d and %d shor from blocks make %d",
		before, after, epoch+1, attestation, epoch, blocks, want)
}
