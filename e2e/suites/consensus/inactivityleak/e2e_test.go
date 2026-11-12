//go:build e2e

package inactivityleak

import (
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Inactivity leak E2E suite")
}

var _ = ginkgo.Describe(
	"A chain with more than a third of its stake offline",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "consensus", "inactivity-leak"),
	func() {
		suite := new(leakSuite)

		ginkgo.BeforeAll(suite.setUp, ginkgo.NodeTimeout(3*time.Minute))
		ginkgo.AfterAll(suite.closeReturned)

		ginkgo.It("finalizes before the stake goes offline", func(ctx ginkgo.SpecContext) {
			testsuite.AwaitFinalizedEpoch(ctx, suite.onlineValidator.Client, suite.chain, 1, finalityTimeoutEpochs)
		}, ginkgo.SpecTimeout(finalityTimeout))

		ginkgo.Context("recovering by one validator returning", func() {
			ginkgo.AfterEach(suite.reportReturnedLogs)

			ginkgo.It("stops finalizing once the validator client stops, while blocks keep coming",
				suite.stopsFinalizing, ginkgo.SpecTimeout(stallTimeout))
			ginkgo.It("leaks an offline validator's stake once finality falls behind",
				suite.leaksOfflineStake, ginkgo.SpecTimeout(leakTimeout))
			ginkgo.It("pays an online validator nothing for its votes during the leak",
				suite.paysOnlineVotesNothing, ginkgo.SpecTimeout(onlineTimeout))
			ginkgo.It("finalizes again once one offline validator returns on a separate validator client",
				suite.finalizesWithOneReturned, ginkgo.SpecTimeout(resumeTimeout))
			ginkgo.It("brings the returned validator's inactivity score back to zero",
				suite.zeroesReturnedScore, ginkgo.SpecTimeout(decayTimeout))
			ginkgo.It("slashes no validator, and every node finalizes the same checkpoint", func(ctx ginkgo.SpecContext) {
				suite.expectCleanFinality(ctx, suite.returning.restarted)
			}, ginkgo.SpecTimeout(checkTimeout))
			ginkgo.It("finalizes with every validator back on its own client",
				suite.finalizesWithEveryValidatorBack, ginkgo.SpecTimeout(rejoinTimeout))
		})

		ginkgo.Context("recovering by the leak alone", func() {
			ginkgo.It("stops finalizing again once the validator client stops for good",
				suite.stopsFinalizingForGood, ginkgo.SpecTimeout(stallTimeout))
			ginkgo.It("forecasts the first epoch the online validators justify alone",
				suite.forecastsRecovery, ginkgo.SpecTimeout(forecastTimeout))
			ginkgo.It("finalizes again within an epoch of the forecast",
				suite.finalizesByLeakAlone, ginkgo.SpecTimeout(recoveryTimeout))
			ginkgo.It("leaks each offline validator's stake as forecast",
				suite.leaksOfflineStakeAsForecast, ginkgo.SpecTimeout(checkTimeout))
			ginkgo.It("slashes no validator, and every node finalizes the same checkpoint", func(ctx ginkgo.SpecContext) {
				suite.expectCleanFinality(ctx, suite.lasting.justified.Epoch)
			}, ginkgo.SpecTimeout(checkTimeout))
		})
	},
)
