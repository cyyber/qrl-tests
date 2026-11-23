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
	"A node with full blocks",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.ContinueOnFailure,
	ginkgo.Label("e2e", "limits", "full-blocks"),
	func() {
		suite := new(fullBlocksSuite)

		ginkgo.BeforeAll(suite.setUp, ginkgo.NodeTimeout(time.Minute))

		ginkgo.It("has the full blocks",
			suite.hasFullBlocks, ginkgo.SpecTimeout(5*time.Minute))
		ginkgo.It("syncs a new node without being dropped as its peer",
			suite.syncsNewNode, ginkgo.SpecTimeout(10*time.Minute))
		ginkgo.It("leaves the network healthy",
			suite.leavesNetworkHealthy, ginkgo.SpecTimeout(5*time.Minute))
	},
)
