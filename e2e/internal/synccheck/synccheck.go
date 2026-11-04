// Package synccheck checks a node that syncs from a live devnet node: that it
// reaches the source's head and then holds the same chain, state and history.
package synccheck

import (
	"context"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/history"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/syncnode"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/params"
)

const (
	reachHeadTimeout  = 6 * time.Minute
	headPolling       = 500 * time.Millisecond
	comparedSamples   = 25
	epochsKeepingHead = 3
)

// Sync is what a node reported on its way to the head, over its API while it
// synced and in its logs after. The logs matter: a devnet's chain downloads in
// well under a second, between two polls.
type Sync struct {
	Path       Path
	Optimistic bool
}

// Path is how the execution client reached the head. Both are false when the
// beacon node fed it every block in order: only its downloader sets a
// qrl_syncing target and logs that a synchronisation started.
type Path struct {
	Downloaded bool
	Snap       bool
}

func (path Path) String() string {
	switch {
	case path.Snap:
		return "snap sync"
	case path.Downloaded:
		return "full sync over devp2p"
	default:
		return "payload by payload"
	}
}

const (
	downloaderLogLine = "Block synchronisation started"
	snapSyncLogLine   = "Syncing: state download in progress"
	optimisticLogLine = "optimistic block"
)

// Origin is where the beacon node started from.
type Origin struct {
	// Slot is the slot of the origin block, the first one the beacon node
	// holds: 0 from genesis, the checkpoint's slot after a checkpoint sync,
	// since Qrysm does not backfill. The execution chain is whole either way:
	// gqrl downloads the headers back to genesis on both of its sync paths.
	Slot uint64
}

var Genesis = Origin{}

// WaitForHead waits until both of the node's clients have the source's head.
func WaitForHead(ctx context.Context, node *syncnode.Node, source *live.Node, chain chaininfo.Info) Sync {
	ginkgo.GinkgoHelper()
	var sync Sync
	gomega.Eventually(func(g gomega.Gomega) {
		progress, err := node.Execution.SyncProgress(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		if progress != nil {
			sync.Path.Downloaded = sync.Path.Downloaded || progress.HighestBlock > 0
			sync.Path.Snap = sync.Path.Snap || progress.SyncedAccounts > 0 || progress.SyncedStorage > 0
		}
		status, err := node.Beacon.SyncStatus(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		sync.Optimistic = sync.Optimistic || status.IsOptimistic

		// Within one block: the source may have just imported the next.
		nodeHead, err := node.Execution.HeaderByNumber(ctx, nil)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		sourceNumber, err := source.Execution.BlockNumber(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(nodeHead.Number.Uint64()+1).To(gomega.BeNumerically(">=", sourceNumber), "execution head")
		sourceHeader, err := source.Execution.HeaderByNumber(ctx, nodeHead.Number)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(nodeHead.Hash()).To(gomega.Equal(sourceHeader.Hash()), "execution head %d", nodeHead.Number)

		nodeBeaconHead, err := node.Beacon.BlockHeader(ctx, "head")
		g.Expect(err).NotTo(gomega.HaveOccurred())
		sourceBeaconHead, err := source.Beacon.BlockHeader(ctx, "head")
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(nodeBeaconHead).To(gomega.Equal(sourceBeaconHead), "beacon head")
	}).WithContext(ctx).
		WithTimeout(reachHeadTimeout).
		WithPolling(headPolling).
		Should(gomega.Succeed())

	executionLog := testsuite.MustSucceed(node.ExecutionLog())
	sync.Path.Downloaded = sync.Path.Downloaded || strings.Contains(executionLog, downloaderLogLine)
	sync.Path.Snap = sync.Path.Snap || strings.Contains(executionLog, snapSyncLogLine)
	beaconLog := testsuite.MustSucceed(node.BeaconLog())
	sync.Optimistic = sync.Optimistic || strings.Contains(beaconLog, optimisticLogLine)
	return sync
}

// ExpectSynced checks a node that has reached the source's head: that it says
// it is synced, holds the same chain, state and history over the range it
// serves, keeps up, and passes on transactions.
func ExpectSynced(ctx context.Context, node *syncnode.Node, source *live.Node, chain chaininfo.Info, written history.History, origin Origin) {
	ginkgo.GinkgoHelper()

	ginkgo.By("reporting synced")
	gomega.Eventually(func(g gomega.Gomega) {
		progress, err := node.Execution.SyncProgress(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(progress).To(gomega.BeNil(), "qrl_syncing")
		status, err := node.Beacon.SyncStatus(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(status.IsSyncing).To(gomega.BeFalse(), "is_syncing")
		g.Expect(status.IsOptimistic).To(gomega.BeFalse(), "is_optimistic")
		g.Expect(status.ELOffline).To(gomega.BeFalse(), "el_offline")
	}).WithContext(ctx).WithTimeout(chain.EpochDuration).WithPolling(chain.SlotDuration).Should(gomega.Succeed())

	ginkgo.By("holding the source's chain")
	head := testsuite.MustSucceed(node.Execution.HeaderByNumber(ctx, nil)).Number.Uint64()
	for _, number := range sampledRange(0, head) {
		height := new(big.Int).SetUint64(number)
		nodeHeader := testsuite.MustSucceed(node.Execution.HeaderByNumber(ctx, height))
		sourceHeader := testsuite.MustSucceed(source.Execution.HeaderByNumber(ctx, height))
		gomega.Expect(nodeHeader.Hash()).To(gomega.Equal(sourceHeader.Hash()), "block %d", number)
	}
	// A beacon node serves blocks by slot from its database, which the
	// blocks it has just synced reach a little later.
	finalizedSlot := testsuite.MustSucceed(node.Beacon.FinalityCheckpoints(ctx)).Finalized.Epoch * chain.SlotsPerEpoch
	gomega.Expect(finalizedSlot).To(gomega.BeNumerically(">=", origin.Slot), "the finalized slot is before the first slot the node serves")
	gomega.Eventually(func(g gomega.Gomega) {
		for _, slot := range sampledRange(origin.Slot, finalizedSlot) {
			expectSameBlockAtSlot(ctx, g, node.Beacon, source.Beacon, slot)
		}
	}).WithContext(ctx).WithTimeout(2 * chain.EpochDuration).WithPolling(chain.SlotDuration).Should(gomega.Succeed())
	if origin.Slot > 0 {
		for _, slot := range sampledRange(0, origin.Slot-1) {
			expectNoOtherBlockAtSlot(ctx, node.Beacon, source.Beacon, slot)
		}
	}
	gomega.Eventually(func(g gomega.Gomega) {
		nodeCheckpoints, err := node.Beacon.FinalityCheckpoints(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		sourceCheckpoints, err := source.Beacon.FinalityCheckpoints(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(nodeCheckpoints.Finalized).To(gomega.Equal(sourceCheckpoints.Finalized), "finalized checkpoint")
		g.Expect(nodeCheckpoints.CurrentJustified).To(gomega.Equal(sourceCheckpoints.CurrentJustified), "justified checkpoint")
	}).WithContext(ctx).WithTimeout(chain.EpochDuration).WithPolling(chain.SlotDuration).Should(gomega.Succeed())

	ginkgo.By("holding the source's state")
	// State is read at the node's head, which both have: a full node keeps the
	// state of its last 128 blocks, and a snap-synced one has none before its
	// pivot, 64 blocks behind the head it started from.
	at := new(big.Int).SetUint64(head)
	for _, account := range append([]common.Address{source.Address}, written.Recipients...) {
		gomega.Expect(node.Execution.BalanceAt(ctx, account, at)).To(
			gomega.Equal(testsuite.MustSucceed(source.Execution.BalanceAt(ctx, account, at))), "balance of %s", account)
		gomega.Expect(node.Execution.NonceAt(ctx, account, at)).To(
			gomega.Equal(testsuite.MustSucceed(source.Execution.NonceAt(ctx, account, at))), "nonce of %s", account)
	}
	code := testsuite.MustSucceed(node.Execution.CodeAt(ctx, written.Contract, at))
	gomega.Expect(code).NotTo(gomega.BeEmpty(), "the history contract has no code")
	gomega.Expect(code).To(gomega.Equal(testsuite.MustSucceed(source.Execution.CodeAt(ctx, written.Contract, at))))
	gomega.Expect(written.Slots).NotTo(gomega.BeEmpty(), "the history filled no storage slot")
	for _, index := range sampledRange(0, uint64(len(written.Slots)-1)) {
		slot := written.Slots[index]
		value := testsuite.MustSucceed(node.Execution.StorageAt(ctx, written.Contract, slot, at))
		gomega.Expect(value).To(gomega.ContainElement(gomega.Not(gomega.BeZero())), "storage slot %s is empty", slot)
		gomega.Expect(value).To(gomega.Equal(
			testsuite.MustSucceed(source.Execution.StorageAt(ctx, written.Contract, slot, at))), "storage slot %s", slot)
	}

	ginkgo.By("holding the source's transactions and receipts")
	// A node indexes transactions in the background once it has synced.
	gomega.Eventually(func(g gomega.Gomega) {
		for _, hash := range written.Transactions {
			nodeReceipt, err := node.Execution.TransactionReceipt(ctx, hash)
			g.Expect(err).NotTo(gomega.HaveOccurred(), "transaction %s", hash)
			sourceReceipt, err := source.Execution.TransactionReceipt(ctx, hash)
			g.Expect(err).NotTo(gomega.HaveOccurred(), "transaction %s", hash)
			g.Expect(summarize(nodeReceipt)).To(gomega.Equal(summarize(sourceReceipt)), "transaction %s", hash)
			_, pending, err := node.Execution.TransactionByHash(ctx, hash)
			g.Expect(err).NotTo(gomega.HaveOccurred(), "transaction %s", hash)
			g.Expect(pending).To(gomega.BeFalse(), "transaction %s", hash)
		}
	}).WithContext(ctx).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(gomega.Succeed())

	ginkgo.By("keeping the source's head")
	for range epochsKeepingHead {
		nextEpoch := chain.Epoch(testsuite.MustSucceed(source.Beacon.HeadSlot(ctx))) + 1
		gomega.Eventually(source.Beacon.HeadSlot).WithContext(ctx).
			WithTimeout(2 * chain.EpochDuration).
			WithPolling(chain.SlotDuration).
			Should(gomega.BeNumerically(">=", nextEpoch*chain.SlotsPerEpoch))
		gomega.Eventually(func(g gomega.Gomega) {
			nodeHead, err := node.Beacon.BlockHeader(ctx, "head")
			g.Expect(err).NotTo(gomega.HaveOccurred())
			sourceHead, err := source.Beacon.BlockHeader(ctx, "head")
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(nodeHead).To(gomega.Equal(sourceHead))
		}).WithContext(ctx).
			WithTimeout(2*chain.SlotDuration).
			WithPolling(headPolling).
			Should(gomega.Succeed(), "head at epoch %d", nextEpoch)
	}

	ginkgo.By("passing a transaction on to the source")
	expectRelayed(ctx, node, source)

	ginkgo.By("logging no bad block")
	// gqrl reports one as "########## BAD BLOCK #########"; the lowercase
	// words also occur in import lines whose block hash ends in "bad".
	logs := testsuite.MustSucceed(node.ExecutionLog())
	gomega.Expect(logs).NotTo(gomega.ContainSubstring("BAD BLOCK"))
}

// expectSameBlockAtSlot fails unless both beacon nodes have the same block at
// slot, or both have none.
func expectSameBlockAtSlot(ctx context.Context, g gomega.Gomega, node, source *beacon.Client, slot uint64) {
	ginkgo.GinkgoHelper()
	blockID := strconv.FormatUint(slot, 10)
	sourceHeader, sourceErr := source.BlockHeader(ctx, blockID)
	nodeHeader, nodeErr := node.BlockHeader(ctx, blockID)
	if beacon.IsNotFound(sourceErr) {
		g.Expect(beacon.IsNotFound(nodeErr)).To(gomega.BeTrue(), "slot %d has no block on the source: %v", slot, nodeErr)
		return
	}
	g.Expect(sourceErr).NotTo(gomega.HaveOccurred(), "slot %d on the source", slot)
	g.Expect(nodeErr).NotTo(gomega.HaveOccurred(), "slot %d", slot)
	g.Expect(nodeHeader).To(gomega.Equal(sourceHeader), "slot %d", slot)
}

// expectNoOtherBlockAtSlot fails if the node has a block at slot that the
// source does not have: a node that does not serve a slot answers not found.
func expectNoOtherBlockAtSlot(ctx context.Context, node, source *beacon.Client, slot uint64) {
	ginkgo.GinkgoHelper()
	blockID := strconv.FormatUint(slot, 10)
	nodeHeader, nodeErr := node.BlockHeader(ctx, blockID)
	if beacon.IsNotFound(nodeErr) {
		return
	}
	gomega.Expect(nodeErr).NotTo(gomega.HaveOccurred(), "slot %d", slot)
	sourceHeader, err := source.BlockHeader(ctx, blockID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "slot %d has a block on the node but not on the source", slot)
	gomega.Expect(nodeHeader).To(gomega.Equal(sourceHeader), "slot %d", slot)
}

// expectRelayed sends a transfer to the node and waits for the source to
// include it.
func expectRelayed(ctx context.Context, node *syncnode.Node, source *live.Node) {
	ginkgo.GinkgoHelper()
	nonce := testsuite.MustSucceed(source.Execution.PendingNonceAt(ctx, source.Address))
	head := testsuite.MustSucceed(source.Execution.HeaderByNumber(ctx, nil))
	tip := testsuite.MustSucceed(source.Execution.SuggestGasTipCap(ctx))
	recipient := common.BytesToAddress([]byte("sync e2e relayed transfer"))
	tx := testsuite.MustSucceed(types.SignNewTx(source.Wallet, types.LatestSignerForChainID(source.ChainID), &types.DynamicFeeTx{
		ChainID:   source.ChainID,
		Nonce:     nonce,
		GasTipCap: tip,
		GasFeeCap: new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), tip),
		Gas:       params.TxGas,
		To:        &recipient,
		Value:     big.NewInt(1),
	}))
	gomega.Expect(node.Execution.SendTransaction(ctx, tx)).To(gomega.Succeed())

	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	receipt, err := bind.WaitMined(waitCtx, source.Execution, tx)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "the source did not include a transaction sent to the node")
	gomega.Expect(receipt.Status).To(gomega.Equal(types.ReceiptStatusSuccessful))
}

// receiptSummary is the part of a receipt both nodes must agree on.
type receiptSummary struct {
	Status            uint64
	BlockHash         common.Hash
	BlockNumber       uint64
	GasUsed           uint64
	CumulativeGasUsed uint64
	ContractAddress   common.Address
	Bloom             types.Bloom
	Logs              []logSummary
}

type logSummary struct {
	Address common.Address
	Topics  []common.LogTopic
	Data    []byte
	Index   uint
}

func summarize(receipt *types.Receipt) receiptSummary {
	summary := receiptSummary{
		Status:            receipt.Status,
		BlockHash:         receipt.BlockHash,
		BlockNumber:       receipt.BlockNumber.Uint64(),
		GasUsed:           receipt.GasUsed,
		CumulativeGasUsed: receipt.CumulativeGasUsed,
		ContractAddress:   receipt.ContractAddress,
		Bloom:             receipt.Bloom,
	}
	for _, log := range receipt.Logs {
		summary.Logs = append(summary.Logs, logSummary{Address: log.Address, Topics: log.Topics, Data: log.Data, Index: log.Index})
	}
	return summary
}

// sampledRange spreads about comparedSamples values from first to last, both
// included.
func sampledRange(first, last uint64) []uint64 {
	if first > last {
		return nil
	}
	step := max((last-first)/comparedSamples, 1)
	var values []uint64
	for value := first; value < last; value += step {
		values = append(values, value)
	}
	return append(values, last)
}
