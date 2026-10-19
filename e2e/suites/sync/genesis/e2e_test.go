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

		ginkgo.It("syncs with gqrl in full mode and Qrysm from genesis", func(ctx ginkgo.SpecContext) {
			gomega.Expect(written.Transactions).NotTo(gomega.BeEmpty(), "there is no history to sync")
			fullNode = startNode(ctx, "sync-full", "full")

			path := waitForHead(ctx, fullNode, source, chain)
			ginkgo.AddReportEntry("execution sync path", path.String())
			expectSynced(ctx, fullNode, source, chain, written)
		}, ginkgo.SpecTimeout(15*time.Minute))

		ginkgo.It("catches up after the source finalizes two more epochs while it is stopped", func(ctx ginkgo.SpecContext) {
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

			path := waitForHead(ctx, fullNode, source, chain)
			ginkgo.AddReportEntry("execution sync path after the restart", path.String())
			expectSynced(ctx, fullNode, source, chain, written)
		}, ginkgo.SpecTimeout(15*time.Minute))
	},
)
