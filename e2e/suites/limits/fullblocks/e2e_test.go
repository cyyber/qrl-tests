//go:build e2e

package fullblocks

import (
	"errors"
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

const (
	// A syncing node keeps up to 256 MB of downloaded blocks; the full blocks
	// must be more.
	keptBlockBytes = 256 << 20
	fullBlocks     = 40
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
			var size uint64
			for _, block := range blocks {
				size += block.EncodedBytes
			}
			ginkgo.AddReportEntry("full blocks", fmt.Sprintf("%d, from block %d to %d, %d MB in all",
				len(blocks), blocks[0].Number, blocks[len(blocks)-1].Number, size>>20))
			gomega.Expect(size).To(gomega.BeNumerically(">", keptBlockBytes), "the full blocks fit in what a syncing node keeps")
		}, ginkgo.SpecTimeout(10*time.Minute))

		ginkgo.It("is synced by a new node without it dropping its peer", func(ctx ginkgo.SpecContext) {
			gomega.Expect(blocks).NotTo(gomega.BeEmpty(), "there are no full blocks to sync")
			node := testsuite.MustSucceed(syncnode.Start(ctx, syncnode.Config{
				Name:           "limits-full-blocks-sync",
				ExecutionImage: runtime.ExecutionImage,
				SyncMode:       "full",
				Source:         source.Participant,
				ExecutionOnly:  true,
			}))
			execution := health.Container{Name: node.Containers()[0], MetricsURL: node.ExecutionMetricsURL}
			ginkgo.DeferCleanup(func(ctx ginkgo.SpecContext) {
				testsuite.ReportLogsOnFailure("execution-only node", node)
				gomega.Expect(errors.Join(monitor.RetireContainer(ctx, execution.Name), node.Close())).To(gomega.Succeed())
			}, ginkgo.NodeTimeout(time.Minute))
			gomega.Expect(monitor.AddContainers(ctx, execution)).To(gomega.Succeed())
			_, id, err := node.Peer(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			syncNodeID = id
			watch := testsuite.MustSucceed(health.WatchPeers(ctx, node.ExecutionWSURL))
			defer watch.Close()

			started := time.Now()
			head := testsuite.MustSucceed(node.SyncToHead(ctx, source.Execution))
			gomega.Eventually(func(g gomega.Gomega) {
				drops, err := watch.Drops(ctx)
				if err != nil {
					gomega.StopTrying("read the sync node's peer drops").Wrap(err).Now()
				}
				if len(drops) > 0 {
					gomega.StopTrying(fmt.Sprintf("the node dropped its peer while it synced: %+v", drops)).Now()
				}
				synced, err := node.Execution.HeaderByNumber(ctx, head.Number)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(synced.Hash()).To(gomega.Equal(head.Hash()))
			}).WithContext(ctx).WithTimeout(15 * time.Minute).WithPolling(time.Second).Should(gomega.Succeed())

			drops := testsuite.MustSucceed(watch.Drops(ctx))
			ginkgo.AddReportEntry("sync", fmt.Sprintf("to block %d in %s, %d peer drops", head.Number, time.Since(started).Round(time.Second), len(drops)))
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
