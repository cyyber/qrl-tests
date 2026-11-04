//go:build e2e

package genesis

import (
	"math/big"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/history"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/syncnode"
	"github.com/cyyber/qrl-tests/e2e/internal/synccheck"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/rpc"
)

const (
	finalizedBlocksBeforeSync   = 200
	epochsFinalizedWhileStopped = 2
)

var historyPlan = history.Plan{Transfers: 20, Fills: 4, SlotsPerFill: 250}

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Genesis sync E2E suite")
}

var _ = ginkgo.Describe(
	"A new node syncing from genesis",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.ContinueOnFailure,
	ginkgo.Label("e2e", "sync", "genesis"),
	func() {
		var (
			runtime  *live.Runtime
			source   *live.Node
			chain    chaininfo.Info
			written  history.History
			snapNode *syncnode.Node
			fullNode *syncnode.Node
		)

		startNode := func(ctx ginkgo.SpecContext, name, syncMode string) *syncnode.Node {
			ginkgo.GinkgoHelper()
			return testsuite.MustSucceed(syncnode.Start(ctx, syncnode.Config{
				Name:           name,
				ExecutionImage: runtime.ExecutionImage,
				ConsensusImage: runtime.ConsensusImage,
				SyncMode:       syncMode,
				Source:         source.Participant,
			}))
		}

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime = testsuite.LoadRuntime()
			gomega.Expect(runtime.ExecutionImage).NotTo(gomega.BeEmpty(), "execution image is not configured")
			gomega.Expect(runtime.ConsensusImage).NotTo(gomega.BeEmpty(), "consensus image is not configured")
			source = testsuite.MustSucceed(runtime.PrimaryNode(ctx))
			chain = testsuite.MustSucceed(chaininfo.Load(ctx, source.Beacon))
		}, ginkgo.NodeTimeout(time.Minute))

		ginkgo.AfterAll(func() {
			gomega.Expect(snapNode.Close()).To(gomega.Succeed())
			gomega.Expect(fullNode.Close()).To(gomega.Succeed())
		})

		ginkgo.AfterEach(func() {
			testsuite.ReportLogsOnFailure("snap-mode node", snapNode)
			testsuite.ReportLogsOnFailure("full-mode node", fullNode)
		})

		ginkgo.It("has a finalized history to sync", func(ctx ginkgo.SpecContext) {
			written = testsuite.MustSucceed(history.Write(ctx, source.Execution, source.Wallet, source.ChainID, historyPlan))

			finalized := big.NewInt(int64(rpc.FinalizedBlockNumber))
			gomega.Eventually(func(g gomega.Gomega) {
				header, err := source.Execution.HeaderByNumber(ctx, finalized)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(header.Number.Uint64()).To(gomega.BeNumerically(">=", finalizedBlocksBeforeSync))
			}).WithContext(ctx).
				WithTimeout(finalizedBlocksBeforeSync*chain.SlotDuration + 8*chain.EpochDuration).
				WithPolling(chain.SlotDuration).
				Should(gomega.Succeed())
		}, ginkgo.SpecTimeout(30*time.Minute))

		ginkgo.It("syncs with the default settings: gqrl in snap mode, Qrysm from genesis", func(ctx ginkgo.SpecContext) {
			gomega.Expect(written.Transactions).NotTo(gomega.BeEmpty(), "there is no history to sync")
			snapNode = startNode(ctx, "sync-snap", "snap")

			sync := synccheck.WaitForHead(ctx, snapNode, source, chain)
			// Snap mode may still import every block on the genesis route; it
			// depends on how far behind the head the first one is. Either way the
			// node must end up the same.
			ginkgo.AddReportEntry("execution sync path", sync.Path.String())
			synccheck.ExpectSynced(ctx, snapNode, source, chain, written, synccheck.Genesis)
		}, ginkgo.SpecTimeout(15*time.Minute))

		ginkgo.It("catches up in snap mode after the source finalizes two more epochs while it is stopped", func(ctx ginkgo.SpecContext) {
			gomega.Expect(snapNode).NotTo(gomega.BeNil(), "the snap-mode node never started")
			// Closed here so it holds no memory while the full-mode node runs.
			ginkgo.DeferCleanup(func() {
				gomega.Expect(snapNode.Close()).To(gomega.Succeed())
				snapNode = nil
			})

			stoppedBlock := testsuite.MustSucceed(snapNode.Execution.BlockNumber(ctx))
			finalizedBefore := testsuite.MustSucceed(source.Beacon.FinalityCheckpoints(ctx)).Finalized.Epoch
			gomega.Expect(snapNode.Stop(ctx)).To(gomega.Succeed())

			gomega.Eventually(func() (uint64, error) {
				checkpoints, err := source.Beacon.FinalityCheckpoints(ctx)
				return checkpoints.Finalized.Epoch, err
			}).WithContext(ctx).
				WithTimeout((epochsFinalizedWhileStopped + 6) * chain.EpochDuration).
				WithPolling(chain.SlotDuration).
				Should(gomega.BeNumerically(">=", finalizedBefore+epochsFinalizedWhileStopped))

			gomega.Expect(snapNode.Resume(ctx)).To(gomega.Succeed())
			gomega.Expect(snapNode.Execution.BlockNumber(ctx)).To(gomega.BeNumerically(">=", stoppedBlock),
				"the execution client lost blocks it had")

			sync := synccheck.WaitForHead(ctx, snapNode, source, chain)
			ginkgo.AddReportEntry("execution sync path after the restart", sync.Path.String())
			synccheck.ExpectSynced(ctx, snapNode, source, chain, written, synccheck.Genesis)
		}, ginkgo.SpecTimeout(15*time.Minute))

		ginkgo.It("syncs with gqrl in full mode and Qrysm from genesis", func(ctx ginkgo.SpecContext) {
			gomega.Expect(written.Transactions).NotTo(gomega.BeEmpty(), "there is no history to sync")
			fullNode = startNode(ctx, "sync-full", "full")

			sync := synccheck.WaitForHead(ctx, fullNode, source, chain)
			ginkgo.AddReportEntry("execution sync path", sync.Path.String())
			synccheck.ExpectSynced(ctx, fullNode, source, chain, written, synccheck.Genesis)
		}, ginkgo.SpecTimeout(15*time.Minute))

		ginkgo.It("catches up in full mode after the source finalizes two more epochs while it is stopped", func(ctx ginkgo.SpecContext) {
			gomega.Expect(fullNode).NotTo(gomega.BeNil(), "the full-mode node never started")
			stoppedBlock := testsuite.MustSucceed(fullNode.Execution.BlockNumber(ctx))
			finalizedBefore := testsuite.MustSucceed(source.Beacon.FinalityCheckpoints(ctx)).Finalized.Epoch
			gomega.Expect(fullNode.Stop(ctx)).To(gomega.Succeed())

			gomega.Eventually(func() (uint64, error) {
				checkpoints, err := source.Beacon.FinalityCheckpoints(ctx)
				return checkpoints.Finalized.Epoch, err
			}).WithContext(ctx).
				WithTimeout((epochsFinalizedWhileStopped + 6) * chain.EpochDuration).
				WithPolling(chain.SlotDuration).
				Should(gomega.BeNumerically(">=", finalizedBefore+epochsFinalizedWhileStopped))

			gomega.Expect(fullNode.Resume(ctx)).To(gomega.Succeed())
			gomega.Expect(fullNode.Execution.BlockNumber(ctx)).To(gomega.BeNumerically(">=", stoppedBlock),
				"the execution client lost blocks it had")

			sync := synccheck.WaitForHead(ctx, fullNode, source, chain)
			ginkgo.AddReportEntry("execution sync path after the restart", sync.Path.String())
			synccheck.ExpectSynced(ctx, fullNode, source, chain, written, synccheck.Genesis)
		}, ginkgo.SpecTimeout(15*time.Minute))
	},
)
