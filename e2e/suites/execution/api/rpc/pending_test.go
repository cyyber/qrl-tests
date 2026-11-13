//go:build e2e

package rpc

import (
	"context"
	"math/big"
	"slices"

	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/suites/execution/abi/contracts"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	qrl "github.com/theQRL/go-qrl"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/params"
	gorpc "github.com/theQRL/go-qrl/rpc"
)

// sendTransfer sends one planck from the shared wallet to the recipient.
func sendTransfer(ctx context.Context, node *live.Node, to common.Address) (*types.Transaction, error) {
	head, err := node.Execution.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, err
	}
	tip, err := node.Execution.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, err
	}
	nonce, err := node.Execution.PendingNonceAt(ctx, node.Address)
	if err != nil {
		return nil, err
	}
	tx, err := types.SignNewTx(node.Wallet, types.NewZondSigner(node.ChainID), &types.DynamicFeeTx{
		ChainID:   node.ChainID,
		Nonce:     nonce,
		GasTipCap: tip,
		// Room for the base fee to double.
		GasFeeCap: new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), tip),
		Gas:       params.TxGas,
		To:        &to,
		Value:     big.NewInt(1),
	})
	if err != nil {
		return nil, err
	}
	return tx, node.Execution.SendTransaction(ctx, tx)
}

// whilePending sends transfers from the shared wallet until read passes for
// one of them. A transfer is pending from when the pool has it until a block
// includes it, so read can come too late for one: another is sent once a
// block has the last.
func whilePending(ctx context.Context, node *live.Node, chain chaininfo.Info, to common.Address, read func(g gomega.Gomega, sent *types.Transaction)) {
	ginkgo.GinkgoHelper()
	var sent *types.Transaction
	gomega.Eventually(func(g gomega.Gomega) {
		if sent != nil {
			if _, err := node.Execution.TransactionReceipt(ctx, sent.Hash()); err == nil {
				sent = nil
			}
		}
		if sent == nil {
			tx, err := sendTransfer(ctx, node, to)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			sent = tx
		}
		read(g, sent)
	}).WithContext(ctx).
		WithTimeout(pendingBlockSlots * chain.SlotDuration).
		WithPolling(pendingBlockPolling).
		Should(gomega.Succeed())
}

// expectPendingBlock reads the pending block while it has a transfer of the
// shared wallet, as qrlclient decodes it and as the API returns it.
func expectPendingBlock(ctx context.Context, node *live.Node, chain chaininfo.Info, to common.Address) {
	ginkgo.GinkgoHelper()
	var (
		block *types.Block
		index int
		raw   map[string]any
		rawTx map[string]any
	)
	whilePending(ctx, node, chain, to, func(g gomega.Gomega, sent *types.Transaction) {
		var err error
		block, err = node.Execution.BlockByNumber(ctx, big.NewInt(int64(gorpc.PendingBlockNumber)))
		g.Expect(err).NotTo(gomega.HaveOccurred())
		index = slices.IndexFunc(block.Transactions(), func(tx *types.Transaction) bool { return tx.Hash() == sent.Hash() })
		g.Expect(index).To(gomega.BeNumerically(">=", 0), "the pending block does not have transaction %s", sent.Hash())

		count, err := node.Execution.PendingTransactionCount(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(count).To(gomega.Equal(uint(len(block.Transactions()))), "transactions of the pending block")

		raw, rawTx = nil, nil
		g.Expect(node.Execution.Client().CallContext(ctx, &raw, "qrl_getBlockByNumber", gorpc.PendingBlockNumber.String(), true)).To(gomega.Succeed())
		for _, tx := range raw["transactions"].([]any) {
			if fields := tx.(map[string]any); fields["hash"] == sent.Hash().Hex() {
				rawTx = fields
			}
		}
		g.Expect(rawTx).NotTo(gomega.BeNil(), "the raw pending block does not have transaction %s", sent.Hash())
	})

	// The pending block is not in the database, so the node cannot answer
	// for its hash: the sender is the one qrlclient kept from the block,
	// under the hash it computed for it.
	sender, err := node.Execution.TransactionSender(ctx, block.Transactions()[index], block.Hash(), uint(index))
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(sender).To(gomega.Equal(node.Address))

	expectHeaderFields(raw, true)
	expectFields(rawTx, transactionFields...)
	gomega.Expect(rawTx).To(gomega.HaveKeyWithValue("from", node.Address.Hex()))
}

// expectNodePendingTransaction reads the pending transactions of the node's
// accounts while the pool has a transfer of the shared wallet, which is the
// node's account. They leave out the pool's transaction of another account.
func expectNodePendingTransaction(ctx context.Context, node *live.Node, chain chaininfo.Info, to common.Address, other *types.Transaction) {
	ginkgo.GinkgoHelper()
	var listed map[string]any
	whilePending(ctx, node, chain, to, func(g gomega.Gomega, sent *types.Transaction) {
		var transactions []map[string]any
		g.Expect(node.Execution.Client().CallContext(ctx, &transactions, "qrl_pendingTransactions")).To(gomega.Succeed())
		listed = nil
		for _, tx := range transactions {
			g.Expect(tx["hash"]).NotTo(gomega.Equal(other.Hash().Hex()), "a transaction of another account")
			if tx["hash"] == sent.Hash().Hex() {
				listed = tx
			}
		}
		g.Expect(listed).NotTo(gomega.BeNil(), "the pending transactions do not have transaction %s", sent.Hash())
	})

	expectFields(listed, transactionFields...)
	gomega.Expect(listed).To(gomega.HaveKeyWithValue("from", node.Address.Hex()))
	for _, field := range []string{"blockHash", "blockNumber", "transactionIndex"} {
		gomega.Expect(listed).To(gomega.HaveKeyWithValue(field, gomega.BeNil()), "field %q", field)
	}
}

// expectPendingState reads the pending state of the account whose
// transaction stays in the pool, and of a contract. No block includes the
// transaction, so the pending state is the latest one, but the pending nonce
// counts it.
func expectPendingState(ctx context.Context, node *live.Node, sender, contract common.Address) {
	ginkgo.GinkgoHelper()
	gomega.Expect(node.Execution.NonceAt(ctx, sender, nil)).To(gomega.BeZero())
	gomega.Expect(node.Execution.PendingNonceAt(ctx, sender)).To(gomega.Equal(uint64(1)))

	balance := testsuite.MustSucceed(node.Execution.BalanceAt(ctx, sender, nil))
	gomega.Expect(balance).To(gomega.Equal(big.NewInt(params.Quanta)))
	gomega.Expect(node.Execution.PendingBalanceAt(ctx, sender)).To(gomega.Equal(balance))

	code := testsuite.MustSucceed(node.Execution.CodeAt(ctx, contract, nil))
	gomega.Expect(code).NotTo(gomega.BeEmpty())
	gomega.Expect(node.Execution.PendingCodeAt(ctx, contract)).To(gomega.Equal(code))

	slot := common.Hash{}
	value := testsuite.MustSucceed(node.Execution.StorageAt(ctx, contract, slot, nil))
	gomega.Expect(node.Execution.PendingStorageAt(ctx, contract, slot)).To(gomega.Equal(value))
}

// expectCalls calls EventEmitter's plusOne, which runs in the library it
// links, at the latest block, at that block's hash and in the pending state.
func expectCalls(ctx context.Context, node *live.Node, emitter common.Address) {
	ginkgo.GinkgoHelper()
	parsed := testsuite.MustSucceed(contracts.EventEmitterMetaData.GetAbi())
	call := qrl.CallMsg{
		From: node.Address,
		To:   &emitter,
		Data: testsuite.MustSucceed(parsed.Pack("plusOne", big.NewInt(41))),
	}
	head := testsuite.MustSucceed(node.Execution.HeaderByNumber(ctx, nil))

	latest := testsuite.MustSucceed(node.Execution.CallContract(ctx, call, nil))
	gomega.Expect(testsuite.MustSucceed(parsed.Unpack("plusOne", latest))).To(gomega.Equal([]any{big.NewInt(42)}))
	gomega.Expect(node.Execution.CallContractAtHash(ctx, call, head.Hash())).To(gomega.Equal(latest))
	gomega.Expect(node.Execution.PendingCallContract(ctx, call)).To(gomega.Equal(latest))
}
