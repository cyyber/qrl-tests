//go:build e2e

package inactivityleak

import (
	"context"
	"fmt"
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

	maxEpochDuration = 40 * time.Second

	finalityTimeoutEpochs = 8
	// Votes cast before the stop are all processed this many epochs after
	// the epoch it happened in; after that, finality cannot move.
	settleEpochs  = 2
	stalledEpochs = 4
	leakEpochs    = 4
	resumeEpochs  = 5
	decayEpochs   = 5

	finalityTimeout = 7 * time.Minute
	stallTimeout    = 6 * time.Minute
	leakTimeout     = 6 * time.Minute
	onlineTimeout   = 3 * time.Minute
	resumeTimeout   = 5 * time.Minute
	decayTimeout    = 8 * time.Minute
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Inactivity leak E2E suite")
}

// The suite ends the leak after a few epochs by restarting the stopped client.
// Letting the leak alone drain the offline stake until the rest finalizes is
// future work: with half the stake offline it takes hours, even at Qrysm's
// INACTIVITY_PENALTY_QUOTIENT of 2^16, though far less on a profile with just
// over a third of the stake offline.
var _ = ginkgo.Describe(
	"A chain with more than a third of its stake offline",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "consensus", "inactivity-leak"),
	func() {
		var (
			runtime *live.Runtime
			nodes   []*live.Node
			chain   chaininfo.Info
			// online runs on the participant that keeps its validator client,
			// offline on the one whose client the suite stops.
			online, offline rewards.Validator
			clientsStopped  bool
			// stopped is the epoch the validator clients stopped in, and
			// restarted the one they started again in.
			stopped, restarted uint64
			frozen             beacon.Checkpoint
			// score is the offline validator's inactivity score after the
			// processing of every epoch before nextEpoch.
			score, nextEpoch uint64
		)

		finalizedEpoch := func(node *live.Node) func(context.Context) (uint64, error) {
			return func(ctx context.Context) (uint64, error) {
				checkpoints, err := node.Beacon.FinalityCheckpoints(ctx)
				return checkpoints.Finalized.Epoch, err
			}
		}

		headEpoch := func(ctx context.Context) (uint64, error) {
			head, err := online.Client.HeadSlot(ctx)
			return chain.Epoch(head), err
		}

		finalizedAtEpochStart := func(ctx context.Context, epoch uint64) beacon.Checkpoint {
			ginkgo.GinkgoHelper()
			stateID := strconv.FormatUint(epoch*chain.SlotsPerEpoch, 10)
			return testsuite.MustSucceed(online.Client.FinalityCheckpointsAt(ctx, stateID)).Finalized
		}

		currentEpoch := func(ctx context.Context) uint64 {
			ginkgo.GinkgoHelper()
			head := testsuite.MustSucceed(online.Client.HeadSlot(ctx))
			return max(chain.EpochAt(time.Now()), chain.Epoch(head))
		}

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime = testsuite.LoadRuntime()
			nodes = testsuite.MustSucceed(runtime.Nodes(ctx))
			gomega.Expect(len(nodes)).To(gomega.BeNumerically(">", offlineParticipant),
				"the suite stops the validator client of participant %d", offlineParticipant+1)
			client := nodes[onlineParticipant].Beacon
			chain = testsuite.MustSucceed(chaininfo.Load(ctx, client))
			gomega.Expect(chain.EpochDuration).To(gomega.BeNumerically("<=", maxEpochDuration),
				"the suite's timeouts assume epochs of at most %s", maxEpochDuration)
			spec := testsuite.MustSucceed(rewards.LoadSpec(ctx, client))

			onlineValidators := runtime.Profile.ParticipantValidators(onlineParticipant)
			offlineValidators := runtime.Profile.ParticipantValidators(offlineParticipant)
			gomega.Expect(onlineValidators).NotTo(gomega.BeEmpty(), "profile %q runs no validator on participant %d", runtime.Profile, onlineParticipant+1)
			gomega.Expect(offlineValidators).NotTo(gomega.BeEmpty(), "profile %q runs no validator on participant %d", runtime.Profile, offlineParticipant+1)
			online = rewards.Validator{Client: client, Chain: chain, Spec: spec, Index: onlineValidators[0]}
			offline = rewards.Validator{Client: client, Chain: chain, Spec: spec, Index: offlineValidators[0]}

			total := testsuite.MustSucceed(client.TotalActiveBalance(ctx, "head"))
			var stake uint64
			for _, index := range offlineValidators {
				stake += testsuite.MustSucceed(client.Validator(ctx, strconv.FormatUint(index, 10))).EffectiveBalance
			}
			// More than a third goes offline, so the rest can't justify.
			gomega.Expect(3*stake > total).To(gomega.BeTrue(),
				"participant %d runs %d of the %d shor staked, not more than a third", offlineParticipant+1, stake, total)
		}, ginkgo.NodeTimeout(time.Minute))

		ginkgo.AfterAll(func(ctx ginkgo.SpecContext) {
			if clientsStopped {
				gomega.Expect(runtime.StartValidator(ctx, offlineParticipant)).To(gomega.Succeed())
			}
		}, ginkgo.NodeTimeout(time.Minute))

		ginkgo.It("finalizes before the stake goes offline", func(ctx ginkgo.SpecContext) {
			gomega.Eventually(finalizedEpoch(nodes[onlineParticipant])).WithContext(ctx).
				WithTimeout(finalityTimeoutEpochs * chain.EpochDuration).
				WithPolling(chain.SlotDuration).
				Should(gomega.BeNumerically(">", 0))
		}, ginkgo.SpecTimeout(finalityTimeout))

		ginkgo.It("stops finalizing while blocks keep coming once the validator client of a participant with more than a third of the stake stops",
			func(ctx ginkgo.SpecContext) {
				gomega.Expect(runtime.StopValidator(ctx, offlineParticipant)).To(gomega.Succeed())
				clientsStopped = true
				stopped = currentEpoch(ctx)

				settled := stopped + settleEpochs
				last := settled + stalledEpochs
				gomega.Eventually(headEpoch).WithContext(ctx).
					WithTimeout((settleEpochs+stalledEpochs+1)*chain.EpochDuration).
					WithPolling(chain.SlotDuration).
					Should(gomega.BeNumerically(">=", last), "the head block has stopped advancing")

				frozen = finalizedAtEpochStart(ctx, settled)
				for epoch := settled + 1; epoch <= last; epoch++ {
					gomega.Expect(finalizedAtEpochStart(ctx, epoch)).To(gomega.Equal(frozen),
						"finalized checkpoint at the start of epoch %d, with the validator client stopped since epoch %d", epoch, stopped)
				}
				ginkgo.GinkgoWriter.Printf("finality stopped at epoch %d after the validator client stopped in epoch %d\n",
					frozen.Epoch, stopped)
			}, ginkgo.SpecTimeout(stallTimeout))

		ginkgo.It("leaks the offline validators' stake once finality is more than MIN_EPOCHS_TO_INACTIVITY_PENALTY epochs behind",
			func(ctx ginkgo.SpecContext) {
				first := offline.Spec.FirstLeakEpoch(frozen.Epoch)
				last := first + leakEpochs - 1
				// The epoch before the leak shows the score starts from zero,
				// and must be one the stopped client missed.
				gomega.Expect(first-1).To(gomega.BeNumerically(">", stopped),
					"the leak starts at epoch %d, too soon after the validator client stopped in epoch %d", first, stopped)
				offline.AwaitRewards(ctx, last)

				amounts := expectMissedEpoch(ctx, offline, first-1, 0)
				gomega.Expect(amounts.Leak).To(gomega.BeFalse(), "epoch %d, %d after finalized epoch %d, is processed in a leak",
					first-1, first-1-frozen.Epoch, frozen.Epoch)
				score = amounts.ScoreAfter
				for epoch := first; epoch <= last; epoch++ {
					amounts := expectMissedEpoch(ctx, offline, epoch, score)
					gomega.Expect(amounts.Leak).To(gomega.BeTrue(), "epoch %d, %d after finalized epoch %d, is not processed in a leak",
						epoch, epoch-frozen.Epoch, frozen.Epoch)
					gomega.Expect(amounts.ReportedLeak).To(gomega.BeTrue(), "the rewards API does not count epoch %d as in a leak", epoch)
					score = amounts.ScoreAfter
				}
				nextEpoch = last + 1
			}, ginkgo.SpecTimeout(leakTimeout))

		ginkgo.It("pays its online validators nothing for their votes during the leak", func(ctx ginkgo.SpecContext) {
			first := online.Spec.FirstLeakEpoch(frozen.Epoch)
			for epoch := first; epoch < first+leakEpochs; epoch++ {
				expectUnpaidVotes(ctx, online, epoch)
			}
		}, ginkgo.SpecTimeout(onlineTimeout))

		ginkgo.It("finalizes again once the stopped validator client starts", func(ctx ginkgo.SpecContext) {
			// Read before the start, so the client cannot have voted in an
			// epoch before restarted.
			restarted = currentEpoch(ctx)
			gomega.Expect(runtime.StartValidator(ctx, offlineParticipant)).To(gomega.Succeed())
			clientsStopped = false

			// The online participant alone holds less than two thirds of the
			// stake, so an epoch from the restart on is justified, and then
			// finalized, only with the restarted client's votes.
			gomega.Eventually(finalizedEpoch(nodes[onlineParticipant])).WithContext(ctx).
				WithTimeout(resumeEpochs*chain.EpochDuration).
				WithPolling(chain.SlotDuration).
				Should(gomega.BeNumerically(">=", restarted), "finalized epoch since the restart in epoch %d", restarted)
		}, ginkgo.SpecTimeout(resumeTimeout))

		ginkgo.It("brings the restarted validators' inactivity scores back to zero", func(ctx ginkgo.SpecContext) {
			for epoch := nextEpoch; epoch <= restarted+decayEpochs; epoch++ {
				offline.AwaitRewards(ctx, epoch)
				if epoch < restarted {
					score = expectMissedEpoch(ctx, offline, epoch, score).ScoreAfter
					continue
				}

				// The client may have missed its first votes after the
				// restart. A timely target vote costs no inactivity penalty,
				// whatever the score.
				reward := testsuite.MustSucceed(offline.AttestationReward(ctx, epoch))
				amounts := testsuite.MustSucceed(offline.LeakVoteAmounts(ctx, epoch))
				// Only a missed target vote is penalized.
				timely := reward.Target >= 0
				wantTarget := amounts.Timely.Target
				if !timely {
					wantTarget = -int64(amounts.MissedTarget + amounts.ReportedInactivity)
				}
				gomega.Expect(reward.Target).To(gomega.Equal(wantTarget),
					"target figure of epoch %d, whose target vote was timely: %t, at score %d", epoch, timely, amounts.ScoreBefore)
				expectScores(offline, epoch, amounts, score, timely)
				score = amounts.ScoreAfter
				if timely && score == 0 {
					ginkgo.GinkgoWriter.Printf("validator %d's inactivity score is back to zero after epoch %d\n", offline.Index, epoch)
					return
				}
			}
			ginkgo.Fail(fmt.Sprintf("validator %d's inactivity score is still %d, %d epochs after the restart in epoch %d",
				offline.Index, score, decayEpochs, restarted))
		}, ginkgo.SpecTimeout(decayTimeout))

		ginkgo.It("slashes no validator, and every node finalizes the same checkpoint", func(ctx ginkgo.SpecContext) {
			for index, node := range nodes {
				gomega.Expect(testsuite.MustSucceed(node.Beacon.SlashedValidators(ctx))).To(gomega.BeEmpty(),
					"validators slashed according to participant %d", index+1)
			}
			// Nodes can cross an epoch boundary a moment apart.
			gomega.Eventually(func(g gomega.Gomega, ctx context.Context) {
				want, err := nodes[0].Beacon.FinalityCheckpoints(ctx)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(want.Finalized.Epoch).To(gomega.BeNumerically(">=", restarted))
				for index, node := range nodes[1:] {
					got, err := node.Beacon.FinalityCheckpoints(ctx)
					g.Expect(err).NotTo(gomega.HaveOccurred())
					g.Expect(got.Finalized).To(gomega.Equal(want.Finalized), "finalized checkpoint of participant %d", index+2)
				}
			}).WithContext(ctx).
				WithTimeout(chain.EpochDuration).
				WithPolling(chain.SlotDuration).
				Should(gomega.Succeed())
		}, ginkgo.SpecTimeout(2*time.Minute))
	},
)
