//go:build e2e

package fullblocks

import (
	"fmt"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/health"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/load"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/syncnode"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/p2p/qnode"
)

// More full blocks than the 256 MB a syncing node keeps of downloaded blocks.
const fullBlocks = 40

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
		var (
			runtime    *live.Runtime
			source     *live.Node
			chain      chaininfo.Info
			monitor    *health.Monitor
			blocks     []load.Block
			syncNodeID qnode.ID
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime = testsuite.LoadRuntime()
			source = testsuite.MustSucceed(runtime.PrimaryNode(ctx))
			chain = testsuite.MustSucceed(chaininfo.Load(ctx, source.Beacon))
			monitor = testsuite.MustSucceed(health.StartMonitor(ctx, source.Participant, source.Beacon, chain))
			ginkgo.DeferCleanup(monitor.Close)
		}, ginkgo.NodeTimeout(time.Minute))

		ginkgo.It("has the full blocks", func(ctx ginkgo.SpecContext) {
			blocks = testsuite.MustSucceed(load.Fill(ctx, source.Execution, source.Wallet, chain.SlotDuration/4, fullBlocks))
			gomega.Expect(len(blocks)).To(gomega.BeNumerically(">=", fullBlocks))
			var size uint64
			for _, block := range blocks {
				size += block.EncodedBytes
			}
			ginkgo.AddReportEntry("full blocks", fmt.Sprintf("%d, from block %d to %d, %d MB in all",
				len(blocks), blocks[0].Number, blocks[len(blocks)-1].Number, size>>20))
		}, ginkgo.SpecTimeout(30*time.Minute))

		ginkgo.It("is synced by a new node without it dropping its peer", func(ctx ginkgo.SpecContext) {
			gomega.Expect(blocks).NotTo(gomega.BeEmpty(), "there are no full blocks to sync")
			node := testsuite.MustSucceed(syncnode.Start(ctx, syncnode.Config{
				Name:           "limits-full-blocks-sync",
				ExecutionImage: runtime.ExecutionImage,
				SyncMode:       "full",
				Source:         source.Participant,
				ExecutionOnly:  true,
			}))
			_, id, err := node.Peer(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			syncNodeID = id
			execution := health.Container{Name: node.Containers()[0], MetricsURL: node.ExecutionMetricsURL}
			gomega.Expect(monitor.AddContainers(ctx, execution)).To(gomega.Succeed())
			ginkgo.DeferCleanup(func(ctx ginkgo.SpecContext) {
				testsuite.ReportLogsOnFailure("execution-only node", node)
				gomega.Expect(monitor.RetireContainer(ctx, execution.Name)).To(gomega.Succeed())
				gomega.Expect(node.Close()).To(gomega.Succeed())
			}, ginkgo.NodeTimeout(time.Minute))
			watch := testsuite.MustSucceed(health.WatchPeers(ctx, node.ExecutionWSURL))
			defer watch.Close()

			started := time.Now()
			head := testsuite.MustSucceed(node.SyncToHead(ctx, source.Execution))
			gomega.Eventually(func(g gomega.Gomega) {
				synced, err := node.Execution.HeaderByNumber(ctx, head.Number)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(synced.Hash()).To(gomega.Equal(head.Hash()))
			}).WithContext(ctx).WithTimeout(15 * time.Minute).WithPolling(time.Second).Should(gomega.Succeed())

			drops := testsuite.MustSucceed(watch.Drops(ctx))
			ginkgo.AddReportEntry("sync", fmt.Sprintf("%d blocks in %s, %d peer drops", head.Number, time.Since(started).Round(time.Second), len(drops)))
			gomega.Expect(drops).To(gomega.BeEmpty(), "the node dropped its peer while it synced")
		}, ginkgo.SpecTimeout(20*time.Minute))

		ginkgo.It("leaves the network healthy", func(ctx ginkgo.SpecContext) {
			// The sync node leaves when its spec ends, which the source sees as
			// a drop.
			report := testsuite.MustSucceed(monitor.Report(ctx, syncNodeID))
			ginkgo.AddReportEntry("network health", report.String())
			// Missed slots are reported, not held against the node: full blocks
			// every six seconds are far more than mainnet's slots ask of it.
			report.MissedSlots = nil
			gomega.Expect(report.Err()).To(gomega.Succeed(), "the network had trouble")
		}, ginkgo.SpecTimeout(5*time.Minute))
	},
)
