//go:build e2e

package inactivityleak

import (
	"context"
	"fmt"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/validator"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const (
	leakEpochs   = 4
	resumeEpochs = 5
	decayEpochs  = 5

	leakTimeout   = 6 * time.Minute
	onlineTimeout = 3 * time.Minute
	resumeTimeout = 5 * time.Minute
	decayTimeout  = 8 * time.Minute
	rejoinTimeout = 9 * time.Minute
)

// returningOutage is the first outage, which one stopped validator returns
// from.
type returningOutage struct {
	// stopped is the epoch the validator client stopped in, and restarted
	// the one the offline validator returned in.
	stopped, restarted uint64
	frozen             beacon.FinalityCheckpoints
	// score is the offline validator's inactivity score after the
	// processing of every epoch before nextEpoch.
	score, nextEpoch uint64
}

func (suite *leakSuite) stopsFinalizing(ctx ginkgo.SpecContext) {
	outage := &suite.returning
	outage.stopped, _, outage.frozen = suite.stopOfflineClient(ctx)
	ginkgo.GinkgoWriter.Printf("finality stopped at epoch %d after the validator client stopped in epoch %d\n",
		outage.frozen.Finalized.Epoch, outage.stopped)
}

func (suite *leakSuite) leaksOfflineStake(ctx ginkgo.SpecContext) {
	outage := &suite.returning
	offline := suite.offlineValidator
	first := offline.Spec.FirstLeakEpoch(outage.frozen.Finalized.Epoch)
	last := first + leakEpochs - 1
	// The epoch before the leak shows the score starts from zero, and must be
	// one the stopped client missed.
	gomega.Expect(first-1).To(gomega.BeNumerically(">", outage.stopped),
		"the leak starts at epoch %d, too soon after the validator client stopped in epoch %d", first, outage.stopped)
	offline.AwaitRewards(ctx, last)

	amounts := expectMissedEpoch(ctx, offline, first-1, 0)
	gomega.Expect(amounts.Leak).To(gomega.BeFalse(), "epoch %d, %d after finalized epoch %d, is processed in a leak",
		first-1, first-1-outage.frozen.Finalized.Epoch, outage.frozen.Finalized.Epoch)
	outage.score = amounts.ScoreAfter
	for epoch := first; epoch <= last; epoch++ {
		amounts := expectMissedEpoch(ctx, offline, epoch, outage.score)
		gomega.Expect(amounts.Leak).To(gomega.BeTrue(), "epoch %d, %d after finalized epoch %d, is not processed in a leak",
			epoch, epoch-outage.frozen.Finalized.Epoch, outage.frozen.Finalized.Epoch)
		outage.score = amounts.ScoreAfter
	}
	outage.nextEpoch = last + 1
}

func (suite *leakSuite) paysOnlineVotesNothing(ctx ginkgo.SpecContext) {
	online := suite.onlineValidator
	first := online.Spec.FirstLeakEpoch(suite.returning.frozen.Finalized.Epoch)
	last := first + leakEpochs - 1
	online.AwaitRewards(ctx, last)
	for epoch := first; epoch <= last; epoch++ {
		expectUnpaidVotes(ctx, online, epoch)
	}
}

func (suite *leakSuite) finalizesWithOneReturned(ctx ginkgo.SpecContext) {
	outage := &suite.returning
	// The online participant holds just under two thirds of the stake, and
	// one more validator takes it over.
	total := testsuite.MustSucceed(suite.onlineValidator.Client.TotalActiveBalance(ctx, "head"))
	onlineStake := suite.effectiveStake(ctx, suite.onlineIndices)
	returning := suite.effectiveStake(ctx, []uint64{suite.offlineValidator.Index})
	gomega.Expect(3*onlineStake < 2*total).To(gomega.BeTrue(),
		"participant %d alone runs %d of the %d shor staked, two thirds or more", onlineParticipant+1, onlineStake, total)
	gomega.Expect(3*(onlineStake+returning) >= 2*total).To(gomega.BeTrue(),
		"participant %d and validator %d run %d of the %d shor staked, less than two thirds",
		onlineParticipant+1, suite.offlineValidator.Index, onlineStake+returning, total)

	// Read before the start, so the validator cannot have voted in an epoch
	// before restarted.
	outage.restarted = suite.currentEpoch(ctx)
	node := suite.nodes[offlineParticipant]
	suite.returnedClient = testsuite.MustSucceed(validator.Start(ctx, validator.Config{
		Image:              node.ValidatorImage,
		BeaconURL:          node.BeaconURL,
		BeaconGRPC:         node.BeaconGRPC,
		ConsensusServiceID: node.ConsensusServiceID,
		Keystores:          suite.returningKeystores,
	}))

	testsuite.AwaitFinalizedEpoch(ctx, suite.onlineValidator.Client, suite.chain, outage.restarted, resumeEpochs)
}

func (suite *leakSuite) zeroesReturnedScore(ctx ginkgo.SpecContext) {
	outage := &suite.returning
	offline := suite.offlineValidator
	for epoch := outage.nextEpoch; epoch <= outage.restarted+decayEpochs; epoch++ {
		offline.AwaitRewards(ctx, epoch)
		if epoch < outage.restarted {
			outage.score = expectMissedEpoch(ctx, offline, epoch, outage.score).ScoreAfter
			continue
		}

		// The client may have missed its first votes after the restart.
		reward := testsuite.MustSucceed(offline.AttestationReward(ctx, epoch))
		amounts := testsuite.MustSucceed(offline.LeakVoteAmounts(ctx, epoch))
		// A timely target vote earns zero or more, and a missed one is
		// penalized; the score check catches a wrong figure.
		timely := reward.Target >= 0
		wantTarget := amounts.Timely.Target
		if !timely {
			wantTarget = -int64(amounts.MissedTarget + amounts.InactivityPenalty)
		}
		gomega.Expect(reward.Target).To(gomega.Equal(wantTarget),
			"target figure of epoch %d, whose target vote was timely: %t, at score %d", epoch, timely, amounts.ScoreAfter)
		expectScores(offline, epoch, amounts, outage.score, timely)
		outage.score = amounts.ScoreAfter
		if timely && outage.score == 0 {
			ginkgo.GinkgoWriter.Printf("validator %d's inactivity score is back to zero after epoch %d\n", offline.Index, epoch)
			return
		}
	}
	ginkgo.Fail(fmt.Sprintf("validator %d's inactivity score is still %d, %d epochs after the restart in epoch %d",
		offline.Index, outage.score, decayEpochs, outage.restarted))
}

func (suite *leakSuite) finalizesWithEveryValidatorBack(ctx ginkgo.SpecContext) {
	suite.closeReturned()
	// The participant's client signs for the returned validator's key from the
	// next epoch on, so no target gets a second vote.
	next := suite.currentEpoch(ctx) + 1
	gomega.Expect(testsuite.SleepUntil(ctx, suite.chain.SlotStartTime(next*suite.chain.SlotsPerEpoch))).To(gomega.Succeed())
	gomega.Expect(suite.runtime.StartParticipantValidators(ctx, offlineParticipant)).To(gomega.Succeed())
	testsuite.AwaitFinalizedEpoch(ctx, suite.onlineValidator.Client, suite.chain, suite.currentEpoch(ctx), finalityTimeoutEpochs)
}

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
