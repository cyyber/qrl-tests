//go:build e2e

package fullblocks

import (
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Full blocks E2E suite")
}

var _ = ginkgo.Describe(
	"A node with a long run of full blocks",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.ContinueOnFailure,
	ginkgo.Label("e2e", "limits", "full-blocks"),
	func() {
		suite := new(fullBlocksSuite)

		ginkgo.BeforeAll(suite.setUp, ginkgo.NodeTimeout(time.Minute))

		ginkgo.It("has the full blocks",
			suite.hasFullBlocks, ginkgo.SpecTimeout(10*time.Minute))
		ginkgo.It("is synced by a new node without it dropping its peer",
			suite.isSyncedByNewNode, ginkgo.SpecTimeout(20*time.Minute))
		ginkgo.It("leaves the network healthy",
			suite.leavesNetworkHealthy, ginkgo.SpecTimeout(5*time.Minute))
	},
)
