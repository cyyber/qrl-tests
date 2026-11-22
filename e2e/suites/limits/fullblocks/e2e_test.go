//go:build e2e

package fullblocks

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/health"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/load"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/syncnode"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/gorilla/websocket"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/p2p/qnode"
)

const (
	// More full blocks than the 256 MB a syncing node keeps of downloaded
	// blocks, and more transactions than the 20,000 notifications a node
	// buffers for a subscriber.
	fullBlocks = 40
	// The blocks one GraphQL query asks for, with every transaction's bytes.
	queriedBlocks = 8
	// The largest reply the suite reads into memory.
	largestReply = 512 << 20
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
			runtime *live.Runtime
			source  *live.Node
			chain   chaininfo.Info
			monitor *health.Monitor
			dense   []load.Block
			stalled *websocket.Conn
			// syncer is the node the sync spec starts and then stops.
			syncer qnode.ID
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime = testsuite.LoadRuntime()
			source = testsuite.MustSucceed(runtime.PrimaryNode(ctx))
			chain = testsuite.MustSucceed(chaininfo.Load(ctx, source.Beacon))
			monitor = testsuite.MustSucceed(health.StartMonitor(ctx, source.Participant, source.Beacon, chain))
			ginkgo.DeferCleanup(monitor.Close)

			// A subscriber to every pending transaction in full, which then
			// reads nothing.
			var err error
			stalled, _, err = websocket.DefaultDialer.DialContext(ctx, source.Participant.Execution.WebSocketURL, nil)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			ginkgo.DeferCleanup(stalled.Close)
			subscribe := `{"jsonrpc":"2.0","id":1,"method":"qrl_subscribe","params":["newPendingTransactions",true]}`
			gomega.Expect(stalled.WriteMessage(websocket.TextMessage, []byte(subscribe))).To(gomega.Succeed())
		}, ginkgo.NodeTimeout(time.Minute))

		ginkgo.It("has the full blocks", func(ctx ginkgo.SpecContext) {
			dense = testsuite.MustSucceed(load.Fill(ctx, source.Execution, source.Wallet, chain.SlotDuration/4, fullBlocks))
			gomega.Expect(len(dense)).To(gomega.BeNumerically(">=", fullBlocks))
			var size uint64
			for _, block := range dense {
				size += block.EncodedBytes
			}
			ginkgo.AddReportEntry("full blocks", fmt.Sprintf("%d, from block %d to %d, %d MB in all",
				len(dense), dense[0].Number, dense[len(dense)-1].Number, size>>20))
		}, ginkgo.SpecTimeout(30*time.Minute))

		ginkgo.It("closes the connection of a subscriber that stopped reading, or still answers on it", func(ctx ginkgo.SpecContext) {
			gomega.Expect(dense).NotTo(gomega.BeEmpty(), "there are no full blocks")
			var sent int
			for _, block := range dense {
				sent += block.Transactions
			}
			// What the node wrote before it gave up is still on the connection.
			var notifications int
			var ended error
			for ended == nil {
				_ = stalled.SetReadDeadline(time.Now().Add(20 * time.Second))
				if _, _, ended = stalled.ReadMessage(); ended == nil {
					notifications++
				}
			}
			ginkgo.AddReportEntry("stalled subscriber", fmt.Sprintf("%d notifications of at least %d transactions, then: %v", notifications, sent, ended))
			gomega.Expect(notifications).To(gomega.BeNumerically("<", sent), "the subscriber did not stall")
			var timeout interface{ Timeout() bool }
			if !errors.As(ended, &timeout) || !timeout.Timeout() {
				return // The node closed the connection.
			}

			// The node kept the connection, so a call on it has to be answered.
			call := `{"jsonrpc":"2.0","id":2,"method":"qrl_chainId","params":[]}`
			gomega.Expect(stalled.WriteMessage(websocket.TextMessage, []byte(call))).To(gomega.Succeed())
			_ = stalled.SetReadDeadline(time.Now().Add(30 * time.Second))
			_, _, err := stalled.ReadMessage()
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "the node keeps the connection but no longer writes to it")
		}, ginkgo.SpecTimeout(10*time.Minute))

		ginkgo.It("answers a GraphQL query for full blocks with their transactions' bytes", func(ctx ginkgo.SpecContext) {
			gomega.Expect(dense).NotTo(gomega.BeEmpty(), "there are no full blocks")
			query := fmt.Sprintf(`{"query":"{ blocks(from: %d, to: %d) { number transactions { raw } } }"}`,
				dense[0].Number, dense[queriedBlocks-1].Number)
			request := testsuite.MustSucceed(http.NewRequestWithContext(ctx, http.MethodPost, source.Participant.Execution.GraphQLURL, bytes.NewReader([]byte(query))))
			request.Header.Set("Content-Type", "application/json")
			started := time.Now()
			response, err := http.DefaultClient.Do(request)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			defer response.Body.Close()
			size, err := io.Copy(io.Discard, io.LimitReader(response.Body, largestReply))
			ginkgo.AddReportEntry("GraphQL query", fmt.Sprintf("%d full blocks: status %d, %d MB in %s, read error: %v",
				queriedBlocks, response.StatusCode, size>>20, time.Since(started).Round(time.Millisecond), err))
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "the reply was cut off")
			gomega.Expect(response.StatusCode).To(gomega.Equal(http.StatusOK))
		}, ginkgo.SpecTimeout(5*time.Minute))

		ginkgo.It("is synced by a new node without it dropping its peer", func(ctx ginkgo.SpecContext) {
			gomega.Expect(dense).NotTo(gomega.BeEmpty(), "there are no full blocks to sync")
			node := testsuite.MustSucceed(syncnode.Start(ctx, syncnode.Config{
				Name:           "limits-full-blocks-sync",
				ExecutionImage: runtime.ExecutionImage,
				SyncMode:       "full",
				Source:         source.Participant,
				ExecutionOnly:  true,
			}))
			_, id, err := node.Peer(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			syncer = id
			execution := health.Container{Name: node.Containers()[0], MetricsURL: node.ExecutionMetricsURL}
			gomega.Expect(monitor.AddContainers(ctx, execution)).To(gomega.Succeed())
			ginkgo.DeferCleanup(func(ctx ginkgo.SpecContext) {
				testsuite.ReportLogsOnFailure("execution-only node", node)
				// Its problems stay in the monitor's report after it is gone.
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
			last := dense[len(dense)-1]
			synced := testsuite.MustSucceed(node.Execution.HeaderByNumber(ctx, new(big.Int).SetUint64(last.Number)))
			gomega.Expect(synced.Hash()).To(gomega.Equal(last.Hash))

			drops := testsuite.MustSucceed(watch.Drops(ctx))
			ginkgo.AddReportEntry("sync", fmt.Sprintf("%d blocks in %s, %d peer drops", head.Number, time.Since(started).Round(time.Second), len(drops)))
			gomega.Expect(drops).To(gomega.BeEmpty(), "the node dropped its peer while it synced")
		}, ginkgo.SpecTimeout(20*time.Minute))

		ginkgo.It("leaves the network healthy", func(ctx ginkgo.SpecContext) {
			// The sync node leaves when its spec ends, which the source sees as
			// a drop.
			report := testsuite.MustSucceed(monitor.Report(ctx, syncer))
			ginkgo.AddReportEntry("network health", report.String())
			// Missed slots are reported, not held against the node: full blocks
			// every six seconds are far more than mainnet's slots ask of it.
			report.MissedSlots = nil
			gomega.Expect(report.Err()).To(gomega.Succeed(), "the network had trouble")
		}, ginkgo.SpecTimeout(5*time.Minute))
	},
)
