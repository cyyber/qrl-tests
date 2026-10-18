//go:build e2e

package defaultnode

import (
	"math/big"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/history"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/syncnode"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/rpc"
)

const (
	// historyBlocks is how long the chain is before a node starts. Snap
	// sync starts 64 blocks behind the head, and a full node keeps the state
	// of the last 128 blocks, so a shorter chain would let snap sync start
	// from a block whose state every node still holds.
	historyBlocks = 200

	// stoppedFinalizedEpochs is how many epochs the source must finalize
	// while the node is stopped.
	stoppedFinalizedEpochs = 2
)

var historyPlan = history.Plan{Transfers: 20, Fills: 4, SlotsPerFill: 250}

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Default sync E2E suite")
}

// The node with gqrl's default snap mode and the node in full mode sync on
// their own, so a failure of one still runs the other and the restart.
var _ = ginkgo.Describe(
	"A new node syncing from genesis",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.ContinueOnFailure,
	ginkgo.Label("e2e", "sync", "sync-default-node"),
	func() {
		var (
			runtime *live.Runtime
			source  *live.Node
			chain   chaininfo.Info
			written history.History
			// snapNode runs gqrl's and Qrysm's default sync settings;
			// fullNode runs gqrl in full mode and is the one restarted.
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
				g.Expect(header.Number.Uint64()).To(gomega.BeNumerically(">=", historyBlocks))
			}).WithContext(ctx).
				WithTimeout(historyBlocks*chain.SlotDuration + 8*chain.EpochDuration).
				WithPolling(chain.SlotDuration).
				Should(gomega.Succeed())
		}, ginkgo.SpecTimeout(30*time.Minute))

		ginkgo.It("syncs with the default settings: gqrl in snap mode, Qrysm from genesis", func(ctx ginkgo.SpecContext) {
			gomega.Expect(written.Transactions).NotTo(gomega.BeEmpty(), "there is no history to sync")
			snapNode = startNode(ctx, "sync-snap", "snap")
			// Removed at the end of the spec, so it holds no memory while the
			// full-mode node runs.
			ginkgo.DeferCleanup(func() {
				gomega.Expect(snapNode.Close()).To(gomega.Succeed())
				snapNode = nil
			})

			path := waitForHead(ctx, snapNode, source, chain)
			// On the genesis route, gqrl in snap mode may still import every
			// block it is handed: that depends on how far behind the head the
			// first one is. Either way the node must end up the same. Until
			// cyyber/go-qrl#278 is fixed it marks block 1 invalid instead.
			ginkgo.AddReportEntry("execution sync path", path.String())
			expectSynced(ctx, snapNode, source, chain, written)
		}, ginkgo.SpecTimeout(15*time.Minute))

		ginkgo.It("syncs with gqrl in full mode and Qrysm from genesis", func(ctx ginkgo.SpecContext) {
			gomega.Expect(written.Transactions).NotTo(gomega.BeEmpty(), "there is no history to sync")
			fullNode = startNode(ctx, "sync-full", "full")

			path := waitForHead(ctx, fullNode, source, chain)
			ginkgo.AddReportEntry("execution sync path", path.String())
			expectSynced(ctx, fullNode, source, chain, written)
		}, ginkgo.SpecTimeout(15*time.Minute))

		ginkgo.It("catches up after it stops for two epochs past a new finalized checkpoint", func(ctx ginkgo.SpecContext) {
			gomega.Expect(fullNode).NotTo(gomega.BeNil(), "the full-mode node never started")
			stoppedAt := testsuite.MustSucceed(fullNode.Execution.BlockNumber(ctx))
			finalizedBefore := testsuite.MustSucceed(source.Beacon.FinalityCheckpoints(ctx)).Finalized.Epoch
			gomega.Expect(fullNode.Stop(ctx)).To(gomega.Succeed())

			gomega.Eventually(func() (uint64, error) {
				checkpoints, err := source.Beacon.FinalityCheckpoints(ctx)
				return checkpoints.Finalized.Epoch, err
			}).WithContext(ctx).
				WithTimeout((stoppedFinalizedEpochs + 6) * chain.EpochDuration).
				WithPolling(chain.SlotDuration).
				Should(gomega.BeNumerically(">=", finalizedBefore+stoppedFinalizedEpochs))

			gomega.Expect(fullNode.Resume(ctx)).To(gomega.Succeed())
			// The node starts from the chain it had, not from genesis, and
			// is behind the source by the epochs it missed.
			gomega.Expect(fullNode.Execution.BlockNumber(ctx)).To(gomega.BeNumerically(">=", stoppedAt),
				"the execution client lost blocks it had")
			nodeHead := testsuite.MustSucceed(fullNode.Beacon.BlockHeader(ctx, "head"))
			sourceHead := testsuite.MustSucceed(source.Beacon.BlockHeader(ctx, "head"))
			gomega.Expect(nodeHead.Slot+chain.SlotsPerEpoch).To(gomega.BeNumerically("<", sourceHead.Slot),
				"the restarted node is not behind")

			path := waitForHead(ctx, fullNode, source, chain)
			ginkgo.AddReportEntry("execution sync path after the restart", path.String())
			expectSynced(ctx, fullNode, source, chain, written)
		}, ginkgo.SpecTimeout(15*time.Minute))
	},
)
