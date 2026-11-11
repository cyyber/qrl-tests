//go:build e2e

package leakrecovery

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const (
	onlineParticipant  = 0
	offlineParticipant = 1

	finalityTimeoutEpochs = 8
	settleEpochs          = 2
	stalledEpochs         = 4
	finalizeEpochs        = 3
	forecastMargin        = 1
	forecastIncrements    = 2
	maxLossDivisor        = 40

	finalityTimeout = 7 * time.Minute
	stallTimeout    = 6 * time.Minute
	forecastTimeout = time.Minute
	recoveryTimeout = 50 * time.Minute
	checkTimeout    = 2 * time.Minute
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Inactivity leak recovery E2E suite")
}

var _ = ginkgo.Describe(
	"A chain with just over a third of its stake offline for good",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "consensus", "inactivity-leak"),
	func() {
		var (
			runtime   *live.Runtime
			nodes     []*live.Node
			chain     chaininfo.Info
			client    *beacon.Client
			spec      rewards.Spec
			increment uint64
			// online runs on the participant that keeps its validator client,
			// offline on the one whose client the suite stops.
			online, offline []uint64
			clientStopped   bool
			// stopped is the epoch the validator client stopped in, and
			// settled the first whose start state has counted every vote cast
			// before the stop.
			stopped, settled uint64
			frozen           beacon.FinalityCheckpoints
			forecast         rewards.Recovery
			// justified is the first checkpoint justified since the stop.
			justified beacon.Checkpoint
		)

		finalizedEpoch := func(ctx context.Context) (uint64, error) {
			checkpoints, err := client.FinalityCheckpoints(ctx)
			return checkpoints.Finalized.Epoch, err
		}

		headEpoch := func(ctx context.Context) (uint64, error) {
			head, err := client.HeadSlot(ctx)
			return chain.Epoch(head), err
		}

		currentEpoch := func(ctx context.Context) uint64 {
			ginkgo.GinkgoHelper()
			head := testsuite.MustSucceed(client.HeadSlot(ctx))
			return max(chain.EpochAt(time.Now()), chain.Epoch(head))
		}

		epochStart := func(epoch uint64) string {
			return strconv.FormatUint(epoch*chain.SlotsPerEpoch, 10)
		}

		checkpointsAtEpochStart := func(ctx context.Context, epoch uint64) beacon.FinalityCheckpoints {
			ginkgo.GinkgoHelper()
			return testsuite.MustSucceed(client.FinalityCheckpointsAt(ctx, epochStart(epoch)))
		}

		// stakeAtEpochStart reads the online validators' effective balance, and
		// the offline validators, from the state at the start of epoch.
		stakeAtEpochStart := func(ctx context.Context, epoch uint64) (uint64, []beacon.Validator) {
			ginkgo.GinkgoHelper()
			var onlineStake uint64
			for _, record := range testsuite.MustSucceed(client.ValidatorsAt(ctx, epochStart(epoch), online)) {
				onlineStake += record.EffectiveBalance
			}
			return onlineStake, testsuite.MustSucceed(client.ValidatorsAt(ctx, epochStart(epoch), offline))
		}

		effectiveStake := func(records []beacon.Validator) uint64 {
			var stake uint64
			for _, record := range records {
				stake += record.EffectiveBalance
			}
			return stake
		}

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime = testsuite.LoadRuntime()
			nodes = testsuite.MustSucceed(runtime.Nodes(ctx))
			gomega.Expect(nodes).To(gomega.HaveLen(2), "the suite stops one of two participants' validator clients")
			client = nodes[onlineParticipant].Beacon
			chain = testsuite.MustSucceed(chaininfo.Load(ctx, client))
			testsuite.ExpectShortEpochs(chain)
			spec = testsuite.MustSucceed(rewards.LoadSpec(ctx, client))
			increment = testsuite.MustSucceed(client.SpecUint(ctx, "EFFECTIVE_BALANCE_INCREMENT"))

			online = runtime.Profile.ParticipantValidators(onlineParticipant)
			offline = runtime.Profile.ParticipantValidators(offlineParticipant)
			gomega.Expect(online).NotTo(gomega.BeEmpty(), "profile %q runs no validator on participant %d", runtime.Profile, onlineParticipant+1)
			gomega.Expect(offline).NotTo(gomega.BeEmpty(), "profile %q runs no validator on participant %d", runtime.Profile, offlineParticipant+1)
		}, ginkgo.NodeTimeout(time.Minute))

		ginkgo.AfterAll(func(ctx ginkgo.SpecContext) {
			if clientStopped {
				gomega.Expect(runtime.StartParticipantValidators(ctx, offlineParticipant)).To(gomega.Succeed())
			}
		}, ginkgo.NodeTimeout(time.Minute))

		ginkgo.It("finalizes before the stake goes offline", func(ctx ginkgo.SpecContext) {
			// An earlier suite on the network may have left finality behind, so
			// wait for it to catch up with the current epoch.
			start := max(currentEpoch(ctx), 1)
			gomega.Eventually(finalizedEpoch).WithContext(ctx).
				WithTimeout(finalityTimeoutEpochs * chain.EpochDuration).
				WithPolling(chain.SlotDuration).
				Should(gomega.BeNumerically(">=", start))
		}, ginkgo.SpecTimeout(finalityTimeout))

		ginkgo.It("stops finalizing with just over a third of the stake offline, while blocks keep coming",
			func(ctx ginkgo.SpecContext) {
				gomega.Expect(runtime.StopParticipantValidators(ctx, offlineParticipant)).To(gomega.Succeed())
				clientStopped = true
				stopped = currentEpoch(ctx)

				// Votes cast before the stop are processed within settleEpochs;
				// after that, finality can't move.
				settled = stopped + settleEpochs
				last := settled + stalledEpochs
				gomega.Eventually(headEpoch).WithContext(ctx).
					WithTimeout((settleEpochs+stalledEpochs+1)*chain.EpochDuration).
					WithPolling(chain.SlotDuration).
					Should(gomega.BeNumerically(">=", last), "the head block has stopped advancing")

				frozen = checkpointsAtEpochStart(ctx, settled)
				for epoch := settled + 1; epoch <= last; epoch++ {
					checkpoints := checkpointsAtEpochStart(ctx, epoch)
					gomega.Expect(checkpoints.Finalized).To(gomega.Equal(frozen.Finalized),
						"finalized checkpoint at the start of epoch %d, with the validator client stopped since epoch %d", epoch, stopped)
					gomega.Expect(checkpoints.CurrentJustified).To(gomega.Equal(frozen.CurrentJustified),
						"justified checkpoint at the start of epoch %d, with the validator client stopped since epoch %d", epoch, stopped)
				}

				onlineStake, offlineRecords := stakeAtEpochStart(ctx, settled)
				offlineStake := effectiveStake(offlineRecords)
				total := testsuite.MustSucceed(client.TotalActiveBalance(ctx, epochStart(settled)))
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
				ginkgo.GinkgoWriter.Printf("finality stopped at epoch %d after the validator client stopped in epoch %d, with %d of the %d shor staked offline\n",
					frozen.Finalized.Epoch, stopped, offlineStake, total)
			}, ginkgo.SpecTimeout(stallTimeout))

		ginkgo.It("forecasts the first epoch the online validators justify alone", func(ctx ginkgo.SpecContext) {
			onlineStake, offlineRecords := stakeAtEpochStart(ctx, settled)
			scores := testsuite.MustSucceed(client.InactivityScores(ctx, epochStart(settled)))
			outage := rewards.Outage{Epoch: settled, Finalized: frozen.Finalized.Epoch, Online: onlineStake}
			for _, record := range offlineRecords {
				gomega.Expect(record.Index).To(gomega.BeNumerically("<", len(scores)), "inactivity scores at the start of epoch %d", settled)
				outage.Offline = append(outage.Offline, rewards.Stake{
					Balance:          record.Balance,
					EffectiveBalance: record.EffectiveBalance,
					InactivityScore:  scores[record.Index],
				})
			}
			forecast = spec.ForecastRecovery(outage)

			// The votes of Justified count at the end of the next epoch, at the
			// effective balances that epoch starts with.
			deciding, ok := forecast.At(forecast.Justified + 1)
			gomega.Expect(ok).To(gomega.BeTrue())
			before, ok := forecast.At(forecast.Justified)
			gomega.Expect(ok).To(gomega.BeTrue())
			ginkgo.GinkgoWriter.Printf("forecast: epoch %d is the first the online validators justify alone, %d epochs after the stop; "+
				"half the online stake is %d shor, and the offline stake %d at the start of epoch %d and %d at the start of epoch %d\n",
				forecast.Justified, forecast.Justified-stopped, onlineStake/2,
				stakeSum(before), forecast.Justified, stakeSum(deciding), forecast.Justified+1)

			gomega.Expect(time.Until(recoveryDeadline(chain, forecast))).To(gomega.BeNumerically("<", recoveryTimeout-checkTimeout),
				"the wait for finality, forecast to justify epoch %d, does not fit the next spec's timeout", forecast.Justified)
		}, ginkgo.SpecTimeout(forecastTimeout))

		ginkgo.It("finalizes again within an epoch of the forecast, with the validator client still stopped",
			func(ctx ginkgo.SpecContext) {
				gomega.Eventually(func(g gomega.Gomega, ctx context.Context) {
					checkpoints, err := client.FinalityCheckpoints(ctx)
					g.Expect(err).NotTo(gomega.HaveOccurred())
					g.Expect(checkpoints.Finalized.Epoch).To(gomega.BeNumerically(">", frozen.Finalized.Epoch),
						"finalized epoch, with epoch %d justified and epoch %d forecast to be", checkpoints.CurrentJustified.Epoch, forecast.Justified)
				}).WithContext(ctx).
					WithTimeout(time.Until(recoveryDeadline(chain, forecast))).
					WithPolling(chain.SlotDuration).
					Should(gomega.Succeed())
				finalized := testsuite.MustSucceed(client.FinalityCheckpoints(ctx)).Finalized

				// The justified checkpoint moves only forward, at epoch starts.
				for epoch := settled + 1; epoch <= testsuite.MustSucceed(headEpoch(ctx)); epoch++ {
					if checkpoints := checkpointsAtEpochStart(ctx, epoch); checkpoints.CurrentJustified != frozen.CurrentJustified {
						justified = checkpoints.CurrentJustified
						break
					}
				}
				gomega.Expect(justified.Epoch).To(gomega.BeNumerically(">", frozen.CurrentJustified.Epoch), "no checkpoint justified since the stop")
				// The forecast holds the online stake fixed, though a late source
				// vote can take an online validator's effective balance down an
				// increment, and spreads the offline validators' sync committee
				// penalties evenly. Both move the stakes by tens of QRL at most,
				// against the roughly 700 the leak takes from the offline stake
				// in the epoch before recovery, so recovery can come an epoch
				// either side.
				gomega.Expect(justified.Epoch).To(gomega.BeNumerically("~", forecast.Justified, forecastMargin),
					"first epoch justified since the stop in epoch %d, against the forecast", stopped)
				gomega.Expect(finalized).To(gomega.Equal(justified),
					"first checkpoint finalized since the stop: the online votes justify the two epochs after the first justified one too")
				ginkgo.GinkgoWriter.Printf("epoch %d justified and finalized, against epoch %d forecast, %d epochs after the validator client stopped in epoch %d\n",
					justified.Epoch, forecast.Justified, justified.Epoch-stopped, stopped)
			}, ginkgo.SpecTimeout(recoveryTimeout))

		ginkgo.It("leaves every offline validator active, at the effective balance the forecast put it", func(ctx ginkgo.SpecContext) {
			// The processing at the end of the epoch after the justified one
			// counted its votes, at the effective balances that epoch starts
			// with.
			deciding := justified.Epoch + 1
			onlineStake, offlineRecords := stakeAtEpochStart(ctx, deciding)
			offlineStake := effectiveStake(offlineRecords)
			gomega.Expect(onlineStake >= 2*offlineStake).To(gomega.BeTrue(),
				"at the start of epoch %d, the online validators hold %d shor and the offline ones %d, more than half as much",
				deciding, onlineStake, offlineStake)

			start, ok := forecast.At(settled)
			gomega.Expect(ok).To(gomega.BeTrue())
			want, ok := forecast.At(deciding)
			gomega.Expect(ok).To(gomega.BeTrue(), "no forecast for epoch %d", deciding)
			for position, record := range offlineRecords {
				// A validator's own sync committee seats move its balance by
				// about a QRL either way from the forecast's even spread, and
				// hysteresis can round that to one more increment.
				gomega.Expect(record.EffectiveBalance).To(gomega.BeNumerically("~", want[position].EffectiveBalance, forecastIncrements*increment),
					"effective balance of validator %d at the start of epoch %d, against the forecast", record.Index, deciding)
				gomega.Expect(maxLossDivisor*(start[position].EffectiveBalance-record.EffectiveBalance)).
					To(gomega.BeNumerically("<", start[position].EffectiveBalance),
						"validator %d's effective balance went from %d to %d shor, down a fortieth or more",
						record.Index, start[position].EffectiveBalance, record.EffectiveBalance)
			}
			ginkgo.GinkgoWriter.Printf("the offline stake went from %d to %d shor by the start of epoch %d, against %d forecast\n",
				stakeSum(start), offlineStake, deciding, stakeSum(want))

			for _, record := range testsuite.MustSucceed(client.ValidatorsAt(ctx, "head", offline)) {
				gomega.Expect(record.Status).To(gomega.Equal("active_ongoing"), "status of offline validator %d", record.Index)
			}
		}, ginkgo.SpecTimeout(checkTimeout))

		ginkgo.It("slashes no validator, and every node finalizes the same checkpoint", func(ctx ginkgo.SpecContext) {
			for index, node := range nodes {
				gomega.Expect(testsuite.MustSucceed(node.Beacon.SlashedValidators(ctx))).To(gomega.BeEmpty(),
					"validators slashed according to participant %d", index+1)
			}
			// Nodes can cross an epoch boundary a moment apart.
			gomega.Eventually(func(g gomega.Gomega, ctx context.Context) {
				want, err := nodes[0].Beacon.FinalityCheckpoints(ctx)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(want.Finalized.Epoch).To(gomega.BeNumerically(">=", justified.Epoch))
				for index, node := range nodes[1:] {
					got, err := node.Beacon.FinalityCheckpoints(ctx)
					g.Expect(err).NotTo(gomega.HaveOccurred())
					g.Expect(got.Finalized).To(gomega.Equal(want.Finalized), "finalized checkpoint of participant %d", index+2)
				}
			}).WithContext(ctx).
				WithTimeout(chain.EpochDuration).
				WithPolling(chain.SlotDuration).
				Should(gomega.Succeed())
		}, ginkgo.SpecTimeout(checkTimeout))
	},
)

// recoveryDeadline is when finality has returned at the latest. The online
// votes justify the two epochs after the first justified one too, so the
// processing at the end of the third finalizes it; the next epoch's first
// slots may be empty, so the deadline allows that epoch whole.
func recoveryDeadline(chain chaininfo.Info, forecast rewards.Recovery) time.Time {
	finalizing := forecast.Justified + forecastMargin + finalizeEpochs
	return chain.SlotStartTime((finalizing + 2) * chain.SlotsPerEpoch)
}

func stakeSum(stakes []rewards.Stake) uint64 {
	var sum uint64
	for _, stake := range stakes {
		sum += stake.EffectiveBalance
	}
	return sum
}
