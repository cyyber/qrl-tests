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

	forecastTimeout = time.Minute
	recoveryTimeout = 50 * time.Minute
)

// lastingOutage is the second outage, which lasts until the leak alone brings
// finality back.
type lastingOutage struct {
	stoppedEpoch, settledEpoch uint64
	frozen                     beacon.FinalityCheckpoints
	forecast                   rewards.RecoveryForecast
	firstJustified             beacon.Checkpoint
}

func (suite *leakSuite) stopsFinalizingForGood(ctx ginkgo.SpecContext) {
	outage := &suite.lasting
	outage.stoppedEpoch = suite.stopValidatorClient(ctx)
	outage.settledEpoch, outage.frozen = suite.expectFrozenFinality(ctx, outage.stoppedEpoch)

	// Just over a third offline: enough to stop finality, but within one
	// validator of a third, so the leak can restore it soon.
	onlineStake, offlineRecords := suite.stakesAtEpochStart(ctx, outage.settledEpoch)
	offlineStake := totalEffectiveBalance(offlineRecords)
	total := testsuite.MustSucceed(suite.onlineBeacon.TotalActiveBalance(ctx, suite.stateAtEpochStart(outage.settledEpoch)))
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
		outage.frozen.Finalized.Epoch, outage.stoppedEpoch, offlineStake, total)
}

func (suite *leakSuite) forecastsRecovery(ctx ginkgo.SpecContext) {
	outage := &suite.lasting
	outage.forecast = suite.forecast(ctx)
	firstJustified := outage.forecast.FirstJustified
	ginkgo.GinkgoWriter.Printf("forecast: epoch %d is the first the online validators justify alone, %d epochs after the stop\n",
		firstJustified, firstJustified-outage.stoppedEpoch)
	gomega.Expect(time.Until(recoveryDeadline(suite.chain, outage.forecast))).To(gomega.BeNumerically("<", recoveryTimeout-checkTimeout),
		"the wait for finality, forecast to justify epoch %d, does not fit the next spec's timeout", firstJustified)
}

func (suite *leakSuite) finalizesByLeakAlone(ctx ginkgo.SpecContext) {
	outage := &suite.lasting
	forecast := outage.forecast
	var finalized beacon.Checkpoint
	outage.firstJustified, finalized = suite.awaitRecovery(ctx)

	suite.expectFirstJustified(ctx)
	gomega.Expect(outage.firstJustified.Epoch).To(gomega.BeNumerically("~", forecast.FirstJustified, forecastMargin),
		"first epoch justified since the stop in epoch %d, against the forecast", outage.stoppedEpoch)
	gomega.Expect(finalized).To(gomega.Equal(outage.firstJustified),
		"first checkpoint finalized since the stop: the online votes justify the two epochs after the first justified one too")
	ginkgo.GinkgoWriter.Printf("epoch %d justified and finalized, against epoch %d forecast, %d epochs after the validator client stopped in epoch %d\n",
		outage.firstJustified.Epoch, forecast.FirstJustified, outage.firstJustified.Epoch-outage.stoppedEpoch, outage.stoppedEpoch)
}

func (suite *leakSuite) leaksOfflineStakeAsForecast(ctx ginkgo.SpecContext) {
	outage := &suite.lasting
	// The first justified epoch's votes count at the next epoch's balances.
	deciding := outage.firstJustified.Epoch + 1
	_, offline := suite.stakesAtEpochStart(ctx, deciding)
	want, ok := outage.forecast.OfflineStakesAt(deciding)
	gomega.Expect(ok).To(gomega.BeTrue(), "no forecast for epoch %d", deciding)
	expectForecastStakes(offline, want, deciding, suite.balanceIncrement)
	ginkgo.GinkgoWriter.Printf("the offline stake was %d shor at the start of epoch %d, against %d forecast\n",
		totalEffectiveBalance(offline), deciding, rewards.TotalEffectiveBalance(want))
}

// forecast reads the chain at the start of the settled epoch and predicts the
// recovery from there.
func (suite *leakSuite) forecast(ctx context.Context) rewards.RecoveryForecast {
	ginkgo.GinkgoHelper()
	outage := suite.lasting
	online, offline := suite.stakesAtEpochStart(ctx, outage.settledEpoch)
	scores := testsuite.MustSucceed(suite.onlineBeacon.InactivityScores(ctx, suite.stateAtEpochStart(outage.settledEpoch)))
	start := rewards.Outage{Epoch: outage.settledEpoch, FinalizedEpoch: outage.frozen.Finalized.Epoch, OnlineStake: online}
	for _, record := range offline {
		gomega.Expect(record.Index).To(gomega.BeNumerically("<", len(scores)), "inactivity scores at the start of epoch %d", outage.settledEpoch)
		start.OfflineStakes = append(start.OfflineStakes, rewards.Stake{
			Balance:          record.Balance,
			EffectiveBalance: record.EffectiveBalance,
			InactivityScore:  scores[record.Index],
		})
	}
	return testsuite.MustSucceed(suite.onlineValidator.Spec.ForecastRecovery(start))
}

// awaitRecovery polls the head until finality moves past the frozen
// checkpoint, and returns the first checkpoint justified since the stop and
// the first finalized. The head shows each justification as it happens;
// states far behind it, finalized by then, are slow to read.
func (suite *leakSuite) awaitRecovery(ctx context.Context) (firstJustified, finalized beacon.Checkpoint) {
	ginkgo.GinkgoHelper()
	outage := suite.lasting
	gomega.Eventually(func(g gomega.Gomega, ctx context.Context) {
		checkpoints, err := suite.onlineBeacon.FinalityCheckpoints(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		if firstJustified == (beacon.Checkpoint{}) && checkpoints.CurrentJustified != outage.frozen.CurrentJustified {
			firstJustified = checkpoints.CurrentJustified
		}
		finalized = checkpoints.Finalized
		g.Expect(finalized.Epoch).To(gomega.BeNumerically(">", outage.frozen.Finalized.Epoch),
			"finalized epoch, with epoch %d justified and epoch %d forecast to justify first",
			checkpoints.CurrentJustified.Epoch, outage.forecast.FirstJustified)
	}).WithContext(ctx).
		WithTimeout(time.Until(recoveryDeadline(suite.chain, outage.forecast))).
		WithPolling(suite.chain.SlotDuration).
		Should(gomega.Succeed())
	return firstJustified, finalized
}

// expectFirstJustified checks that the second outage's justified checkpoint is
// the first since the stop: the epoch after it still starts with the frozen
// one. That state is after the finalized checkpoint, so the node keeps it at
// hand.
func (suite *leakSuite) expectFirstJustified(ctx context.Context) {
	ginkgo.GinkgoHelper()
	outage := suite.lasting
	gomega.Expect(outage.firstJustified.Epoch).To(gomega.BeNumerically(">", outage.frozen.CurrentJustified.Epoch),
		"no checkpoint justified since the stop in epoch %d", outage.stoppedEpoch)
	gomega.Expect(suite.checkpointsAtEpochStart(ctx, outage.firstJustified.Epoch+1).CurrentJustified).To(gomega.Equal(outage.frozen.CurrentJustified),
		"justified checkpoint at the start of epoch %d, before epoch %d's votes count", outage.firstJustified.Epoch+1, outage.firstJustified.Epoch)
}

// expectForecastStakes checks the offline validators' effective balances at
// the start of epoch against the forecast's. A validator's own sync committee
// seats move its balance by about a QRL either way from the forecast's even
// spread, and hysteresis can round that to one more increment.
func expectForecastStakes(records []beacon.Validator, want []rewards.Stake, epoch, increment uint64) {
	ginkgo.GinkgoHelper()
	for position, record := range records {
		gomega.Expect(record.EffectiveBalance).To(gomega.BeNumerically("~", want[position].EffectiveBalance, forecastIncrements*increment),
			"effective balance of validator %d at the start of epoch %d, against the forecast", record.Index, epoch)
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
