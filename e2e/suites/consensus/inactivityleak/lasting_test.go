//go:build e2e

package inactivityleak

import (
	"context"
	"strconv"
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
	forecast         rewards.Recovery
	// justified is the first checkpoint justified since the stop.
	justified beacon.Checkpoint
}

func (suite *leakSuite) stopsFinalizingForGood(ctx ginkgo.SpecContext) {
	outage, states := &suite.lasting, suite.states
	gomega.Expect(suite.runtime.StopParticipantValidators(ctx, offlineParticipant)).To(gomega.Succeed())
	outage.stopped = suite.currentEpoch(ctx)

	outage.settled = outage.stopped + settleEpochs
	last := outage.settled + stalledEpochs
	gomega.Eventually(suite.headEpoch).WithContext(ctx).
		WithTimeout((settleEpochs+stalledEpochs+1)*suite.chain.EpochDuration).
		WithPolling(suite.chain.SlotDuration).
		Should(gomega.BeNumerically(">=", last), "the head block has stopped advancing")

	outage.frozen = states.checkpointsAt(ctx, outage.settled)
	for epoch := outage.settled + 1; epoch <= last; epoch++ {
		checkpoints := states.checkpointsAt(ctx, epoch)
		gomega.Expect(checkpoints.Finalized).To(gomega.Equal(outage.frozen.Finalized),
			"finalized checkpoint at the start of epoch %d, with the validator client stopped since epoch %d", epoch, outage.stopped)
		gomega.Expect(checkpoints.CurrentJustified).To(gomega.Equal(outage.frozen.CurrentJustified),
			"justified checkpoint at the start of epoch %d, with the validator client stopped since epoch %d", epoch, outage.stopped)
	}

	// Just over a third: more than a third offline, but not without its
	// largest validator.
	onlineStake, offlineRecords := states.at(ctx, outage.settled)
	offlineStake := totalEffectiveBalance(offlineRecords)
	total := testsuite.MustSucceed(suite.onlineValidator.Client.TotalActiveBalance(ctx, states.stateAt(outage.settled)))
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
	outage.forecast = suite.states.forecast(ctx, suite.onlineValidator.Spec, *outage)
	forecast := outage.forecast

	// The votes of Justified count at the end of the next epoch, at the
	// effective balances that epoch starts with.
	before, ok := forecast.At(forecast.Justified)
	gomega.Expect(ok).To(gomega.BeTrue())
	deciding, ok := forecast.At(forecast.Justified + 1)
	gomega.Expect(ok).To(gomega.BeTrue())
	onlineStake, _ := suite.states.at(ctx, outage.settled)
	ginkgo.GinkgoWriter.Printf("forecast: epoch %d is the first the online validators justify alone, %d epochs after the stop; "+
		"half the online stake is %d shor, and the offline stake %d at the start of epoch %d and %d at the start of epoch %d\n",
		forecast.Justified, forecast.Justified-outage.stopped, onlineStake/2,
		rewards.TotalEffectiveBalance(before), forecast.Justified, rewards.TotalEffectiveBalance(deciding), forecast.Justified+1)

	gomega.Expect(time.Until(recoveryDeadline(suite.chain, forecast))).To(gomega.BeNumerically("<", recoveryTimeout-checkTimeout),
		"the wait for finality, forecast to justify epoch %d, does not fit the next spec's timeout", forecast.Justified)
}

func (suite *leakSuite) finalizesByLeakAlone(ctx ginkgo.SpecContext) {
	outage := &suite.lasting
	forecast := outage.forecast
	// The head shows each justification as it happens, at most one an epoch,
	// which states long behind it, by then finalized, would be slow to show.
	var finalized beacon.Checkpoint
	gomega.Eventually(func(g gomega.Gomega, ctx context.Context) {
		checkpoints, err := suite.onlineValidator.Client.FinalityCheckpoints(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		if outage.justified == (beacon.Checkpoint{}) && checkpoints.CurrentJustified != outage.frozen.CurrentJustified {
			outage.justified = checkpoints.CurrentJustified
		}
		finalized = checkpoints.Finalized
		g.Expect(finalized.Epoch).To(gomega.BeNumerically(">", outage.frozen.Finalized.Epoch),
			"finalized epoch, with epoch %d justified and epoch %d forecast to be", checkpoints.CurrentJustified.Epoch, forecast.Justified)
	}).WithContext(ctx).
		WithTimeout(time.Until(recoveryDeadline(suite.chain, forecast))).
		WithPolling(suite.chain.SlotDuration).
		Should(gomega.Succeed())

	suite.states.expectFirstJustified(ctx, *outage)
	// The forecast holds the online stake fixed, though a late source vote can
	// take an online validator's effective balance down an increment, and
	// spreads the offline validators' sync committee penalties evenly. Both
	// move the stakes by tens of QRL at most, against the roughly 700 the leak
	// takes from the offline stake in the epoch before recovery, so recovery
	// can come an epoch either side.
	gomega.Expect(outage.justified.Epoch).To(gomega.BeNumerically("~", forecast.Justified, forecastMargin),
		"first epoch justified since the stop in epoch %d, against the forecast", outage.stopped)
	gomega.Expect(finalized).To(gomega.Equal(outage.justified),
		"first checkpoint finalized since the stop: the online votes justify the two epochs after the first justified one too")
	ginkgo.GinkgoWriter.Printf("epoch %d justified and finalized, against epoch %d forecast, %d epochs after the validator client stopped in epoch %d\n",
		outage.justified.Epoch, forecast.Justified, outage.justified.Epoch-outage.stopped, outage.stopped)
}

func (suite *leakSuite) keepsOfflineValidatorsActive(ctx ginkgo.SpecContext) {
	outage := &suite.lasting
	// The processing at the end of the epoch after the justified one counted
	// its votes, at the effective balances that epoch starts with.
	deciding := outage.justified.Epoch + 1
	onlineStake, offlineRecords := suite.states.at(ctx, deciding)
	offlineStake := totalEffectiveBalance(offlineRecords)
	gomega.Expect(onlineStake >= 2*offlineStake).To(gomega.BeTrue(),
		"at the start of epoch %d, the online validators hold %d shor and the offline ones %d, more than half as much",
		deciding, onlineStake, offlineStake)

	start, ok := outage.forecast.At(outage.settled)
	gomega.Expect(ok).To(gomega.BeTrue())
	want, ok := outage.forecast.At(deciding)
	gomega.Expect(ok).To(gomega.BeTrue(), "no forecast for epoch %d", deciding)
	expectForecastStakes(offlineRecords, start, want, deciding, suite.balanceIncrement)
	ginkgo.GinkgoWriter.Printf("the offline stake went from %d to %d shor by the start of epoch %d, against %d forecast\n",
		rewards.TotalEffectiveBalance(start), offlineStake, deciding, rewards.TotalEffectiveBalance(want))

	for _, record := range testsuite.MustSucceed(suite.onlineValidator.Client.ValidatorsAt(ctx, "head", suite.offlineIndices)) {
		gomega.Expect(record.Status).To(gomega.Equal("active_ongoing"), "status of offline validator %d", record.Index)
	}
}

func (suite *leakSuite) agreesAfterLeak(ctx ginkgo.SpecContext) {
	suite.expectCleanFinality(ctx, suite.lasting.justified.Epoch)
}

// beaconStates reads the online and the offline validators, and finality,
// from one node's beacon states.
type beaconStates struct {
	client          *beacon.Client
	chain           chaininfo.Info
	online, offline []uint64
}

func (states beaconStates) stateAt(epoch uint64) string {
	return strconv.FormatUint(epoch*states.chain.SlotsPerEpoch, 10)
}

// checkpointsAt reads the finality checkpoints at the start of epoch.
func (states beaconStates) checkpointsAt(ctx context.Context, epoch uint64) beacon.FinalityCheckpoints {
	ginkgo.GinkgoHelper()
	return testsuite.MustSucceed(states.client.FinalityCheckpointsAt(ctx, states.stateAt(epoch)))
}

// at reads the online validators' effective balance, and the offline
// validators, at the start of epoch.
func (states beaconStates) at(ctx context.Context, epoch uint64) (uint64, []beacon.Validator) {
	ginkgo.GinkgoHelper()
	online := totalEffectiveBalance(testsuite.MustSucceed(states.client.ValidatorsAt(ctx, states.stateAt(epoch), states.online)))
	return online, testsuite.MustSucceed(states.client.ValidatorsAt(ctx, states.stateAt(epoch), states.offline))
}

// forecast forecasts the outage's recovery from the state at the start of
// its settled epoch.
func (states beaconStates) forecast(ctx context.Context, spec rewards.Spec, outage lastingOutage) rewards.Recovery {
	ginkgo.GinkgoHelper()
	online, offline := states.at(ctx, outage.settled)
	scores := testsuite.MustSucceed(states.client.InactivityScores(ctx, states.stateAt(outage.settled)))
	start := rewards.Outage{Epoch: outage.settled, Finalized: outage.frozen.Finalized.Epoch, Online: online}
	for _, record := range offline {
		gomega.Expect(record.Index).To(gomega.BeNumerically("<", len(scores)), "inactivity scores at the start of epoch %d", outage.settled)
		start.Offline = append(start.Offline, rewards.Stake{
			Balance:          record.Balance,
			EffectiveBalance: record.EffectiveBalance,
			InactivityScore:  scores[record.Index],
		})
	}
	return testsuite.MustSucceed(spec.ForecastRecovery(start))
}

// expectFirstJustified checks that the outage's justified checkpoint is the
// first since the stop: the epoch after it still starts with the frozen one.
// That state is after the finalized checkpoint, so the node keeps it at hand.
func (states beaconStates) expectFirstJustified(ctx context.Context, outage lastingOutage) {
	ginkgo.GinkgoHelper()
	gomega.Expect(outage.justified.Epoch).To(gomega.BeNumerically(">", outage.frozen.CurrentJustified.Epoch),
		"no checkpoint justified since the stop in epoch %d", outage.stopped)
	gomega.Expect(states.checkpointsAt(ctx, outage.justified.Epoch+1).CurrentJustified).To(gomega.Equal(outage.frozen.CurrentJustified),
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
func recoveryDeadline(chain chaininfo.Info, forecast rewards.Recovery) time.Time {
	finalizing := forecast.Justified + forecastMargin + finalizeEpochs
	return chain.SlotStartTime((finalizing + 2) * chain.SlotsPerEpoch)
}

func totalEffectiveBalance(records []beacon.Validator) uint64 {
	var stake uint64
	for _, record := range records {
		stake += record.EffectiveBalance
	}
	return stake
}
