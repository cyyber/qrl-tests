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
		ginkgo.AfterEach(suite.reportReturnedLogs)
		ginkgo.AfterAll(suite.endOutage, ginkgo.NodeTimeout(2*time.Minute))

		ginkgo.Context("until one stopped validator returns", func() {
			ginkgo.It("finalizes before the stake goes offline",
				suite.finalizesBeforeOutage, ginkgo.SpecTimeout(finalityTimeout))
			ginkgo.It("stops finalizing while blocks keep coming",
				suite.stopsFinalizing, ginkgo.SpecTimeout(stallTimeout))
			ginkgo.It("leaks an offline validator's stake once finality falls behind",
				suite.leaksOfflineStake, ginkgo.SpecTimeout(leakTimeout))
			ginkgo.It("pays an online validator nothing for its votes during the leak",
				suite.paysOnlineVotesNothing, ginkgo.SpecTimeout(onlineTimeout))
			ginkgo.It("finalizes again once one stopped validator returns",
				suite.finalizesWithOneReturned, ginkgo.SpecTimeout(resumeTimeout))
			ginkgo.It("brings a restarted validator's inactivity score back to zero",
				suite.zeroesReturnedScore, ginkgo.SpecTimeout(decayTimeout))
			ginkgo.It("slashes no validator, and every node finalizes the same checkpoint",
				suite.agreesAfterReturn, ginkgo.SpecTimeout(checkTimeout))
		})

		ginkgo.Context("with the client stopped for good", func() {
			ginkgo.It("finalizes with every validator back on its own client",
				suite.finalizesWithEveryValidatorBack, ginkgo.SpecTimeout(rejoinTimeout))
			ginkgo.It("stops finalizing again once the client stops for good",
				suite.stopsFinalizingForGood, ginkgo.SpecTimeout(stallTimeout))
			ginkgo.It("forecasts the first epoch the online validators justify alone",
				suite.forecastsRecovery, ginkgo.SpecTimeout(forecastTimeout))
			ginkgo.It("finalizes again within an epoch of the forecast, with no validator back",
				suite.finalizesByLeakAlone, ginkgo.SpecTimeout(recoveryTimeout))
			ginkgo.It("leaves every offline validator active, at the effective balance the forecast put it",
				suite.keepsOfflineValidatorsActive, ginkgo.SpecTimeout(checkTimeout))
			ginkgo.It("slashes no validator after the leak alone, and every node finalizes the same checkpoint",
				suite.agreesAfterLeak, ginkgo.SpecTimeout(checkTimeout))
		})
	},
)
