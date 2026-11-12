//go:build e2e

package inactivityleak

import (
	"context"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const (
	finalizeEpochs     = 3
	forecastMargin     = 1
	forecastIncrements = 2
	maxLossDivisor     = 40

	forecastTimeout = time.Minute
	recoveryTimeout = 50 * time.Minute
)

// lastingOutage is the second outage, which starts once every validator is
// back and which no validator returns from.
type lastingOutage struct {
	// stopped is the epoch the validator client stopped in, and settled the
	// first whose start state has counted every vote cast before the stop.
	stopped, settled uint64
	frozen           beacon.FinalityCheckpoints
	forecast         rewards.RecoveryForecast
	// justified is the first checkpoint justified since the stop.
	justified beacon.Checkpoint
}

func (suite *leakSuite) stopsFinalizingForGood(ctx ginkgo.SpecContext) {
	outage := &suite.lasting
	outage.stopped, outage.settled, outage.frozen = suite.stopOfflineClient(ctx)

	// Just over a third: more than a third offline, but not without its
	// largest validator.
	onlineStake, offlineRecords := suite.stakesAt(ctx, outage.settled)
	offlineStake := totalEffectiveBalance(offlineRecords)
	total := testsuite.MustSucceed(suite.onlineBeacon.TotalActiveBalance(ctx, suite.stateAt(outage.settled)))
	gomega.Expect(onlineStake+offlineStake).To(gomega.Equal(total), "the participants' validators hold all the active stake")
	gomega.Expect(3*offlineStake > total).To(gomega.BeTrue(),
		"participant %d runs %d of the %d shor staked, not more than a third", offlineParticipant+1, offlineStake, total)
	var largest uint64
	for _, record := range offlineRecords {
		largest = max(largest, record.EffectiveBalance)
	}
	gomega.Expect(3*(offlineStake-largest) <= total).To(gomega.BeTrue(),
		"participant %d runs %d of the %d shor staked, more than a third even without its largest validator",
		offlineParticipant+1, offlineStake, total)
	ginkgo.GinkgoWriter.Printf("finality stopped at epoch %d after the validator client stopped again in epoch %d, with %d of the %d shor staked offline\n",
		outage.frozen.Finalized.Epoch, outage.stopped, offlineStake, total)
}

func (suite *leakSuite) forecastsRecovery(ctx ginkgo.SpecContext) {
	outage := &suite.lasting
	outage.forecast = suite.forecast(ctx)
	forecast := outage.forecast

	// The votes of FirstJustified count at the end of the next epoch, at the
	// effective balances that epoch starts with.
	justifiedStakes, ok := forecast.OfflineStakesAt(forecast.FirstJustified)
	gomega.Expect(ok).To(gomega.BeTrue())
	decidingStakes, ok := forecast.OfflineStakesAt(forecast.FirstJustified + 1)
	gomega.Expect(ok).To(gomega.BeTrue())
	onlineStake, _ := suite.stakesAt(ctx, outage.settled)
	ginkgo.GinkgoWriter.Printf("forecast: epoch %d is the first the online validators justify alone, %d epochs after the stop; "+
		"half the online stake is %d shor, and the offline stake %d at the start of epoch %d and %d at the start of epoch %d\n",
		forecast.FirstJustified, forecast.FirstJustified-outage.stopped, onlineStake/2,
		rewards.TotalEffectiveBalance(justifiedStakes), forecast.FirstJustified, rewards.TotalEffectiveBalance(decidingStakes), forecast.FirstJustified+1)

	gomega.Expect(time.Until(recoveryDeadline(suite.chain, forecast))).To(gomega.BeNumerically("<", recoveryTimeout-checkTimeout),
		"the wait for finality, forecast to justify epoch %d, does not fit the next spec's timeout", forecast.FirstJustified)
}

func (suite *leakSuite) finalizesByLeakAlone(ctx ginkgo.SpecContext) {
	outage := &suite.lasting
	forecast := outage.forecast
	// Poll the head, which shows each justification as it happens; states far
	// behind it, finalized by then, are slow to read.
	var finalized beacon.Checkpoint
	gomega.Eventually(func(g gomega.Gomega, ctx context.Context) {
		checkpoints, err := suite.onlineBeacon.FinalityCheckpoints(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		if outage.justified == (beacon.Checkpoint{}) && checkpoints.CurrentJustified != outage.frozen.CurrentJustified {
			outage.justified = checkpoints.CurrentJustified
		}
		finalized = checkpoints.Finalized
		g.Expect(finalized.Epoch).To(gomega.BeNumerically(">", outage.frozen.Finalized.Epoch),
			"finalized epoch, with epoch %d justified and epoch %d forecast to be", checkpoints.CurrentJustified.Epoch, forecast.FirstJustified)
	}).WithContext(ctx).
		WithTimeout(time.Until(recoveryDeadline(suite.chain, forecast))).
		WithPolling(suite.chain.SlotDuration).
		Should(gomega.Succeed())

	suite.expectFirstJustified(ctx)
	// The forecast holds the online stake fixed, though a late source vote can
	// take an online validator's effective balance down an increment, and
	// spreads the offline validators' sync committee penalties evenly. Both
	// move the stakes by tens of QRL at most, against the roughly 700 the leak
	// takes from the offline stake in the epoch before recovery, so recovery
	// can come an epoch either side.
	gomega.Expect(outage.justified.Epoch).To(gomega.BeNumerically("~", forecast.FirstJustified, forecastMargin),
		"first epoch justified since the stop in epoch %d, against the forecast", outage.stopped)
	gomega.Expect(finalized).To(gomega.Equal(outage.justified),
		"first checkpoint finalized since the stop: the online votes justify the two epochs after the first justified one too")
	ginkgo.GinkgoWriter.Printf("epoch %d justified and finalized, against epoch %d forecast, %d epochs after the validator client stopped in epoch %d\n",
		outage.justified.Epoch, forecast.FirstJustified, outage.justified.Epoch-outage.stopped, outage.stopped)
}

func (suite *leakSuite) leaksOfflineStakeAsForecast(ctx ginkgo.SpecContext) {
	outage := &suite.lasting
	// The processing at the end of the epoch after the justified one counted
	// its votes, at the effective balances that epoch starts with.
	deciding := outage.justified.Epoch + 1
	_, offlineRecords := suite.stakesAt(ctx, deciding)

	start, ok := outage.forecast.OfflineStakesAt(outage.settled)
	gomega.Expect(ok).To(gomega.BeTrue())
	want, ok := outage.forecast.OfflineStakesAt(deciding)
	gomega.Expect(ok).To(gomega.BeTrue(), "no forecast for epoch %d", deciding)
	expectForecastStakes(offlineRecords, start, want, deciding, suite.balanceIncrement)
	ginkgo.GinkgoWriter.Printf("the offline stake went from %d to %d shor by the start of epoch %d, against %d forecast\n",
		rewards.TotalEffectiveBalance(start), totalEffectiveBalance(offlineRecords), deciding, rewards.TotalEffectiveBalance(want))
}

// forecast forecasts the second outage's recovery from the state at the start
// of its settled epoch.
func (suite *leakSuite) forecast(ctx context.Context) rewards.RecoveryForecast {
	ginkgo.GinkgoHelper()
	outage := suite.lasting
	online, offline := suite.stakesAt(ctx, outage.settled)
	scores := testsuite.MustSucceed(suite.onlineBeacon.InactivityScores(ctx, suite.stateAt(outage.settled)))
	start := rewards.Outage{Epoch: outage.settled, FinalizedEpoch: outage.frozen.Finalized.Epoch, OnlineStake: online}
	for _, record := range offline {
		gomega.Expect(record.Index).To(gomega.BeNumerically("<", len(scores)), "inactivity scores at the start of epoch %d", outage.settled)
		start.OfflineStakes = append(start.OfflineStakes, rewards.Stake{
			Balance:          record.Balance,
			EffectiveBalance: record.EffectiveBalance,
			InactivityScore:  scores[record.Index],
		})
	}
	return testsuite.MustSucceed(suite.onlineValidator.Spec.ForecastRecovery(start))
}

// expectFirstJustified checks that the second outage's justified checkpoint is
// the first since the stop: the epoch after it still starts with the frozen
// one. That state is after the finalized checkpoint, so the node keeps it at
// hand.
func (suite *leakSuite) expectFirstJustified(ctx context.Context) {
	ginkgo.GinkgoHelper()
	outage := suite.lasting
	gomega.Expect(outage.justified.Epoch).To(gomega.BeNumerically(">", outage.frozen.CurrentJustified.Epoch),
		"no checkpoint justified since the stop in epoch %d", outage.stopped)
	gomega.Expect(suite.checkpointsAt(ctx, outage.justified.Epoch+1).CurrentJustified).To(gomega.Equal(outage.frozen.CurrentJustified),
		"justified checkpoint at the start of epoch %d, before epoch %d's votes count", outage.justified.Epoch+1, outage.justified.Epoch)
}

// expectForecastStakes checks the offline validators' effective balances at
// the start of epoch against the forecast's. A validator's own sync committee
// seats move its balance by about a QRL either way from the forecast's even
// spread, and hysteresis can round that to one more increment.
func expectForecastStakes(records []beacon.Validator, start, want []rewards.Stake, epoch, increment uint64) {
	ginkgo.GinkgoHelper()
	for position, record := range records {
		gomega.Expect(record.EffectiveBalance).To(gomega.BeNumerically("~", want[position].EffectiveBalance, forecastIncrements*increment),
			"effective balance of validator %d at the start of epoch %d, against the forecast", record.Index, epoch)
		gomega.Expect(maxLossDivisor*(start[position].EffectiveBalance-record.EffectiveBalance)).
			To(gomega.BeNumerically("<", start[position].EffectiveBalance),
				"validator %d's effective balance went from %d to %d shor, down a fortieth or more",
				record.Index, start[position].EffectiveBalance, record.EffectiveBalance)
	}
}

// recoveryDeadline is when finality has returned at the latest. The online
// votes justify the two epochs after the first justified one too, so the
// processing at the end of the third finalizes it; the next epoch's first
// slots may be empty, so the deadline allows that epoch whole.
func recoveryDeadline(chain chaininfo.Info, forecast rewards.RecoveryForecast) time.Time {
	finalizing := forecast.FirstJustified + forecastMargin + finalizeEpochs
	return chain.SlotStartTime((finalizing + 2) * chain.SlotsPerEpoch)
}
