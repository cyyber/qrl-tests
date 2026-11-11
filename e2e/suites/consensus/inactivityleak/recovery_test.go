//go:build e2e

package inactivityleak

import (
	"context"
	"strconv"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// lastingOutage is the second outage, which no validator returns from.
type lastingOutage struct {
	// stopped is the epoch the validator client stopped in, and settled the
	// first whose start state has counted every vote cast before the stop.
	stopped, settled uint64
	frozen           beacon.FinalityCheckpoints
	forecast         rewards.Recovery
	// justified is the first checkpoint justified since the stop.
	justified beacon.Checkpoint
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
	return spec.ForecastRecovery(start)
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

// expectCleanFinality checks that no validator is slashed, and that every
// node finalizes the same checkpoint, at epoch since or later.
func expectCleanFinality(ctx context.Context, nodes []*live.Node, chain chaininfo.Info, since uint64) {
	ginkgo.GinkgoHelper()
	for index, node := range nodes {
		gomega.Expect(testsuite.MustSucceed(node.Beacon.SlashedValidators(ctx))).To(gomega.BeEmpty(),
			"validators slashed according to participant %d", index+1)
	}
	// Nodes can cross an epoch boundary a moment apart.
	gomega.Eventually(func(g gomega.Gomega, ctx context.Context) {
		want, err := nodes[0].Beacon.FinalityCheckpoints(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(want.Finalized.Epoch).To(gomega.BeNumerically(">=", since))
		for index, node := range nodes[1:] {
			got, err := node.Beacon.FinalityCheckpoints(ctx)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(got.Finalized).To(gomega.Equal(want.Finalized), "finalized checkpoint of participant %d", index+2)
		}
	}).WithContext(ctx).
		WithTimeout(chain.EpochDuration).
		WithPolling(chain.SlotDuration).
		Should(gomega.Succeed())
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

func forecastTotal(stakes []rewards.Stake) uint64 {
	var stake uint64
	for _, entry := range stakes {
		stake += entry.EffectiveBalance
	}
	return stake
}
