//go:build e2e

package fullblocks

import (
	"fmt"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/health"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/syncnode"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

func (suite *fullBlocksSuite) isSyncedByNewNode(ctx ginkgo.SpecContext) {
	gomega.Expect(suite.blocks).NotTo(gomega.BeEmpty(), "there are no full blocks to sync")
	node := testsuite.MustSucceed(syncnode.Start(ctx, syncnode.Config{
		Name:           "limits-full-blocks-sync",
		ExecutionImage: suite.source.ExecutionImage,
		SyncMode:       "full",
		Source:         suite.source.Participant,
		ExecutionOnly:  true,
	}))
	ginkgo.DeferCleanup(func() {
		testsuite.ReportLogsOnFailure("execution-only node", node)
		gomega.Expect(node.Close()).To(gomega.Succeed())
	})
	execution := health.Container{Name: node.Containers()[0], MetricsURL: node.ExecutionMetricsURL}
	gomega.Expect(suite.monitor.AddContainers(ctx, execution)).To(gomega.Succeed())
	ginkgo.DeferCleanup(suite.monitor.RetireContainer, execution.Name, ginkgo.NodeTimeout(time.Minute))
	_, id, err := node.Peer(ctx)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	suite.syncNodeID = id
	watch := testsuite.MustSucceed(health.WatchPeers(ctx, node.ExecutionWSURL))
	defer watch.Close()

	started := time.Now()
	head := testsuite.MustSucceed(node.SyncToHead(ctx, suite.source.Execution))
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
	}).WithContext(ctx).WithTimeout(5 * time.Minute).WithPolling(time.Second).Should(gomega.Succeed())

	drops := testsuite.MustSucceed(watch.Drops(ctx))
	ginkgo.AddReportEntry("sync", fmt.Sprintf("to block %d in %s, %d peer drops", head.Number, time.Since(started).Round(time.Second), len(drops)))
	gomega.Expect(drops).To(gomega.BeEmpty(), "the node dropped its peer while it synced")
}
