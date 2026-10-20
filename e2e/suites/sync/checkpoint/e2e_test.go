//go:build e2e

package checkpoint

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

const epochsFinalizedWhileStopped = 2

var historyPlan = history.Plan{Transfers: 20, Fills: 4, SlotsPerFill: 250}

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Checkpoint sync E2E suite")
}

var _ = ginkgo.Describe(
	"A new node syncing from a checkpoint",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.ContinueOnFailure,
	ginkgo.Label("e2e", "sync", "checkpoint"),
	func() {
		var (
			runtime  *live.Runtime
			source   *live.Node
			chain    chaininfo.Info
			written  history.History
			fullNode *syncnode.Node
			// checkpointSlot is the slot of the running node's origin block.
			checkpointSlot uint64
		)

		// startNode returns the node and the slot of its checkpoint block, its
		// finalized block right after it starts.
		startNode := func(ctx ginkgo.SpecContext, name, syncMode string) (*syncnode.Node, uint64) {
			ginkgo.GinkgoHelper()
			finalizedBefore := testsuite.MustSucceed(source.Beacon.FinalityCheckpoints(ctx)).Finalized
			node := testsuite.MustSucceed(syncnode.Start(ctx, syncnode.Config{
				Name:           name,
				ExecutionImage: runtime.ExecutionImage,
				ConsensusImage: runtime.ConsensusImage,
				SyncMode:       syncMode,
				BeaconOrigin:   syncnode.Checkpoint,
				Source:         source.Participant,
			}))
			origin := testsuite.MustSucceed(node.Beacon.BlockHeader(ctx, "finalized"))
			finalizedAfter := testsuite.MustSucceed(source.Beacon.FinalityCheckpoints(ctx)).Finalized
			gomega.Expect(origin.Root).To(gomega.BeElementOf(finalizedBefore.Root, finalizedAfter.Root),
				"the node did not start from the source's finalized checkpoint")
			gomega.Expect(origin.Slot).To(gomega.BeNumerically(">", 0), "the node started from genesis")
			ginkgo.AddReportEntry("checkpoint slot", origin.Slot)
			return node, origin.Slot
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
			testsuite.ReportLogsOnFailure("full-mode node", fullNode)
		})

		// The history must be behind the checkpoint, so a node starting from it
		// has to download the chain to hold the history.
		ginkgo.It("has a finalized history to sync", func(ctx ginkgo.SpecContext) {
			written = testsuite.MustSucceed(history.Write(ctx, source.Execution, source.Wallet, source.ChainID, historyPlan))
			writtenBy := testsuite.MustSucceed(source.Execution.BlockNumber(ctx))

			finalized := big.NewInt(int64(rpc.FinalizedBlockNumber))
			gomega.Eventually(func(g gomega.Gomega) {
				header, err := source.Execution.HeaderByNumber(ctx, finalized)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(header.Number.Uint64()).To(gomega.BeNumerically(">=", writtenBy))
			}).WithContext(ctx).
				WithTimeout(8 * chain.EpochDuration).
				WithPolling(chain.SlotDuration).
				Should(gomega.Succeed())
		}, ginkgo.SpecTimeout(10*time.Minute))

		// The beacon node starts at the checkpoint and holds no block before it,
		// so gqrl cannot be fed the chain payload by payload: it downloads the
		// whole chain from its peer over devp2p and, in full mode, executes every
		// block of it.
		ginkgo.It("syncs with gqrl in full mode and Qrysm from a checkpoint", func(ctx ginkgo.SpecContext) {
			gomega.Expect(written.Transactions).NotTo(gomega.BeEmpty(), "there is no history to sync")
			fullNode, checkpointSlot = startNode(ctx, "sync-full-checkpoint", "full")

			sync := synccheck.WaitForHead(ctx, fullNode, source, chain)
			ginkgo.AddReportEntry("execution sync path", sync.Path.String())
			gomega.Expect(sync.Path.Downloaded).To(gomega.BeTrue(),
				"neither qrl_syncing nor the log shows the execution client downloading the chain")
			gomega.Expect(sync.Path.Snap).To(gomega.BeFalse(), "a full-mode node synced state by snapshot")
			// The beacon node holds the checkpoint's blocks before its execution
			// client has executed them, and must say so.
			gomega.Expect(sync.Optimistic).To(gomega.BeTrue(),
				"the beacon node never reported optimistic while its execution client caught up")
			synccheck.ExpectSynced(ctx, fullNode, source, chain, written, synccheck.Origin{Slot: checkpointSlot})
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
			synccheck.ExpectSynced(ctx, fullNode, source, chain, written, synccheck.Origin{Slot: checkpointSlot})
		}, ginkgo.SpecTimeout(15*time.Minute))
	},
)
