//go:build e2e

package fullblocks

import (
	"fmt"

	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/health"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/load"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/p2p/qnode"
)

const (
	downloaderBlockCache = 256 << 20
	fullBlocks           = 48
)

type fullBlocksSuite struct {
	source     *live.Node
	chain      chaininfo.Info
	monitor    *health.Monitor
	blocks     []load.Block
	syncNodeID qnode.ID
}

func (suite *fullBlocksSuite) setUp(ctx ginkgo.SpecContext) {
	runtime := testsuite.LoadRuntime()
	suite.source = testsuite.MustSucceed(runtime.PrimaryNode(ctx))
	suite.chain = testsuite.MustSucceed(chaininfo.Load(ctx, suite.source.Beacon))
	suite.monitor = testsuite.MustSucceed(health.StartMonitor(ctx, suite.source.Participant, suite.source.Beacon, suite.chain))
	ginkgo.DeferCleanup(suite.monitor.Close)
}

func (suite *fullBlocksSuite) hasFullBlocks(ctx ginkgo.SpecContext) {
	source := suite.source
	suite.blocks = testsuite.MustSucceed(load.Fill(ctx, source.Execution, source.Wallet, suite.chain.SlotDuration/4, fullBlocks))
	var size uint64
	for _, block := range suite.blocks {
		size += block.EncodedBytes
	}
	ginkgo.AddReportEntry("full blocks", fmt.Sprintf("%d, from block %d to %d, %d MB in all",
		len(suite.blocks), suite.blocks[0].Number, suite.blocks[len(suite.blocks)-1].Number, size>>20))
	gomega.Expect(size).To(gomega.BeNumerically(">", downloaderBlockCache), "the full blocks fit in the downloader's block cache")
}

func (suite *fullBlocksSuite) leavesNetworkHealthy(ctx ginkgo.SpecContext) {
	// The sync node leaves when its spec ends, which the source sees as a drop.
	report := testsuite.MustSucceed(suite.monitor.Report(ctx, suite.syncNodeID))
	ginkgo.AddReportEntry("network health", report.String())
	// Missed slots are reported, not held against the node: full blocks every
	// six seconds are far more than mainnet's slots ask of it.
	report.MissedSlots = nil
	gomega.Expect(report.Err()).To(gomega.Succeed(), "the network had trouble")
}
