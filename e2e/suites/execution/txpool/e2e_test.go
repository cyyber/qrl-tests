//go:build e2e

package txpool

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	qrl "github.com/theQRL/go-qrl"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
	qrlwallet "github.com/theQRL/go-qrl/crypto/pqcrypto/wallet"
	"github.com/theQRL/go-qrl/params"
)

const (
	// priceBump is the pool's default --txpool.pricebump, in percent.
	priceBump = 10

	// The pool's error text; core/txpool cannot be imported here.
	replaceUnderpriced = "replacement transaction underpriced"

	// pollInterval is how often the specs ask the node for a new block.
	pollInterval = time.Second
	// stuckAfterBlocks is how many blocks an unmined transaction is left
	// before it counts as stuck. Normal fees take at most two.
	stuckAfterBlocks = 3
)

// Fees of a stuck transaction, in planck. The base fee never drops below 7,
// so no block includes it. The tip is lower, so it can be raised on its own.
var (
	stuckFeeCap = big.NewInt(4)
	stuckTip    = big.NewInt(2)
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Transaction pool E2E suite")
}

// feeCase is a named pair of fees for a replacement transaction that the pool
// must reject.
type feeCase struct {
	name        string
	feeCap, tip *big.Int
}

var _ = ginkgo.Describe(
	"Transaction replacement against a live qrl-package network",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "txpool"),
	func() {
		var (
			node          *live.Node
			signer        types.Signer
			sender        qrlwallet.Wallet
			senderAddress common.Address
			feeCap, tip   *big.Int
			stuck         *types.Transaction

			recipient         = common.BytesToAddress([]byte("txpool e2e recipient"))
			mistakenRecipient = common.BytesToAddress([]byte("txpool e2e mistaken"))
		)

		transfer := func(
			wallet qrlwallet.Wallet,
			nonce uint64,
			to common.Address,
			value, gasFeeCap, gasTipCap *big.Int,
		) *types.Transaction {
			ginkgo.GinkgoHelper()
			return testsuite.MustSucceed(types.SignNewTx(wallet, signer, &types.DynamicFeeTx{
				ChainID:   node.ChainID,
				Nonce:     nonce,
				GasTipCap: gasTipCap,
				GasFeeCap: gasFeeCap,
				Gas:       params.TxGas,
				To:        &to,
				Value:     value,
			}))
		}
		// payment sends one planck from the sender.
		payment := func(nonce uint64, to common.Address, gasFeeCap, gasTipCap *big.Int) *types.Transaction {
			ginkgo.GinkgoHelper()
			return transfer(sender, nonce, to, big.NewInt(1), gasFeeCap, gasTipCap)
		}
		// cancellation sends nothing from the sender to itself: it takes a
		// nonce and does nothing else.
		cancellation := func(nonce uint64, gasFeeCap, gasTipCap *big.Int) *types.Transaction {
			ginkgo.GinkgoHelper()
			return transfer(sender, nonce, senderAddress, new(big.Int), gasFeeCap, gasTipCap)
		}

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			node = testsuite.MustSucceed(testsuite.LoadRuntime().PrimaryNode(ctx))
			signer = types.LatestSignerForChainID(node.ChainID)
			head := testsuite.MustSucceed(node.Execution.HeaderByNumber(ctx, nil))
			tip = testsuite.MustSucceed(node.Execution.SuggestGasTipCap(ctx))
			// Room for the base fee to double.
			feeCap = new(big.Int).Mul(head.BaseFee, big.NewInt(2))
			feeCap.Add(feeCap, tip)

			ginkgo.By("funding a new account to send the transactions")
			// A stuck transaction blocks its sender's later nonces, so the
			// shared wallet does not send them.
			sender = testsuite.MustSucceed(qrlwallet.Generate(qrlwallet.ML_DSA_87))
			senderAddress = common.Address(sender.GetAddress())
			funding := big.NewInt(params.Quanta)
			walletNonce := testsuite.MustSucceed(node.Execution.PendingNonceAt(ctx, node.Address))
			fund := transfer(node.Wallet, walletNonce, senderAddress, funding, feeCap, tip)
			gomega.Expect(node.Execution.SendTransaction(ctx, fund)).To(gomega.Succeed())
			testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, fund))
		}, ginkgo.NodeTimeout(2*time.Minute))

		// On a congested network a waiting transaction is first outbid by
		// higher tips, and then left behind for good once the base fee rises
		// above its fee cap. This simulates the second: the transaction starts
		// with a fee cap below the base fee.
		ginkgo.It("leaves a transaction with a fee cap below the base fee out of blocks", func(ctx ginkgo.SpecContext) {
			stuck = payment(0, recipient, stuckFeeCap, stuckTip)
			gomega.Expect(node.Execution.SendTransaction(ctx, stuck)).To(gomega.Succeed())

			for range stuckAfterBlocks {
				waitForBlock(ctx, node)
			}
			expectInPool(ctx, node, stuck, "stuck")
			// The pending nonce moved: the transaction has the sender's next
			// nonce, and only its fees keep it out of a block.
			gomega.Expect(node.Execution.PendingNonceAt(ctx, senderAddress)).To(gomega.Equal(uint64(1)))
		}, ginkgo.SpecTimeout(3*time.Minute))

		ginkgo.It("rejects a replacement transaction that does not raise both fees", func(ctx ginkgo.SpecContext) {
			for _, fees := range []feeCase{
				{"the same fees", stuckFeeCap, stuckTip},
				{"only the fee cap raised", bump(stuckFeeCap), stuckTip},
				{"only the tip raised", stuckFeeCap, bump(stuckTip)},
			} {
				err := node.Execution.SendTransaction(ctx, cancellation(0, fees.feeCap, fees.tip))
				gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring(replaceUnderpriced)),
					"replacement transaction with %s", fees.name)
			}
			expectInPool(ctx, node, stuck, "stuck")
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("mines a replacement transaction whose fees cover the base fee", func(ctx ginkgo.SpecContext) {
			before := testsuite.MustSucceed(node.Execution.BalanceAt(ctx, recipient, nil))
			raised := payment(0, recipient, feeCap, tip)
			gomega.Expect(node.Execution.SendTransaction(ctx, raised)).To(gomega.Succeed())

			receipt := testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, raised))
			gomega.Expect(receipt.Status).To(gomega.Equal(types.ReceiptStatusSuccessful))
			expectNotMined(ctx, node, stuck, "stuck")
			gomega.Expect(testsuite.MustSucceed(node.Execution.BalanceAt(ctx, recipient, receipt.BlockNumber))).To(
				gomega.Equal(new(big.Int).Add(before, big.NewInt(1))))
		}, ginkgo.SpecTimeout(2*time.Minute))

		// A transaction sent by mistake is cancelled the same way: a transfer of
		// nothing to its own sender takes its nonce, so it can never be mined.
		// The mistaken transaction has the stuck fees, so no block includes it
		// before it is replaced.
		ginkgo.It("cancels a stuck transaction with a zero-value transfer to its sender", func(ctx ginkgo.SpecContext) {
			nonce := testsuite.MustSucceed(node.Execution.PendingNonceAt(ctx, senderAddress))
			before := testsuite.MustSucceed(node.Execution.BalanceAt(ctx, mistakenRecipient, nil))
			mistaken := payment(nonce, mistakenRecipient, stuckFeeCap, stuckTip)
			gomega.Expect(node.Execution.SendTransaction(ctx, mistaken)).To(gomega.Succeed())
			expectInPool(ctx, node, mistaken, "mistaken")

			cancel := cancellation(nonce, feeCap, tip)
			gomega.Expect(node.Execution.SendTransaction(ctx, cancel)).To(gomega.Succeed())
			expectReplaced(ctx, node, mistaken, "mistaken")

			receipt := testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, cancel))
			gomega.Expect(receipt.Status).To(gomega.Equal(types.ReceiptStatusSuccessful))
			expectNotMined(ctx, node, mistaken, "mistaken")
			gomega.Expect(testsuite.MustSucceed(node.Execution.BalanceAt(ctx, mistakenRecipient, receipt.BlockNumber))).To(
				gomega.Equal(before))
			gomega.Expect(testsuite.MustSucceed(node.Execution.NonceAt(ctx, senderAddress, receipt.BlockNumber))).To(
				gomega.Equal(nonce + 1))
		}, ginkgo.SpecTimeout(2*time.Minute))

		// A transaction sent one nonce ahead waits in the pool's queue, where no
		// block can include it, until the gap is filled. Its sender cancels it
		// there, and the cancelling transfer is mined once the gap is filled.
		ginkgo.It("cancels a transaction sent one nonce ahead", func(ctx ginkgo.SpecContext) {
			nonce := testsuite.MustSucceed(node.Execution.PendingNonceAt(ctx, senderAddress))
			before := testsuite.MustSucceed(node.Execution.BalanceAt(ctx, mistakenRecipient, nil))
			mistaken := payment(nonce+1, mistakenRecipient, feeCap, tip)
			gomega.Expect(node.Execution.SendTransaction(ctx, mistaken)).To(gomega.Succeed())
			expectInPool(ctx, node, mistaken, "mistaken")
			// The pending nonce does not move: the transaction is queued.
			gomega.Expect(node.Execution.PendingNonceAt(ctx, senderAddress)).To(gomega.Equal(nonce))

			// With normal fees the price bump is far more than a planck, so the
			// "one planck short" cases pin it exactly.
			for _, fees := range []feeCase{
				{"the same fees", feeCap, tip},
				{"the fee cap one planck short of the price bump", belowBump(feeCap), bump(tip)},
				{"the tip one planck short of the price bump", bump(feeCap), belowBump(tip)},
			} {
				err := node.Execution.SendTransaction(ctx, cancellation(nonce+1, fees.feeCap, fees.tip))
				gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring(replaceUnderpriced)),
					"cancelling transfer with %s", fees.name)
			}

			cancel := cancellation(nonce+1, bump(feeCap), bump(tip))
			gomega.Expect(node.Execution.SendTransaction(ctx, cancel)).To(gomega.Succeed())
			expectReplaced(ctx, node, mistaken, "mistaken")

			ginkgo.By("filling the nonce gap")
			gomega.Expect(node.Execution.SendTransaction(ctx, cancellation(nonce, feeCap, tip))).To(gomega.Succeed())

			receipt := testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, cancel))
			gomega.Expect(receipt.Status).To(gomega.Equal(types.ReceiptStatusSuccessful))
			expectNotMined(ctx, node, mistaken, "mistaken")
			gomega.Expect(testsuite.MustSucceed(node.Execution.BalanceAt(ctx, mistakenRecipient, receipt.BlockNumber))).To(
				gomega.Equal(before))
			gomega.Expect(testsuite.MustSucceed(node.Execution.NonceAt(ctx, senderAddress, receipt.BlockNumber))).To(
				gomega.Equal(nonce + 2))
		}, ginkgo.SpecTimeout(2*time.Minute))
	},
)

// bump raises fee by the price bump, and by at least one planck.
func bump(fee *big.Int) *big.Int {
	bumped := new(big.Int).Mul(fee, big.NewInt(100+priceBump))
	bumped.Div(bumped, big.NewInt(100))
	if bumped.Cmp(fee) <= 0 {
		bumped.Add(fee, big.NewInt(1))
	}
	return bumped
}

// belowBump raises fee by one planck less than the price bump.
func belowBump(fee *big.Int) *big.Int {
	return new(big.Int).Sub(bump(fee), big.NewInt(1))
}

// expectInPool fails unless the transaction is in the pool, not yet mined.
func expectInPool(ctx context.Context, node *live.Node, tx *types.Transaction, name string) {
	ginkgo.GinkgoHelper()
	_, isPending, err := node.Execution.TransactionByHash(ctx, tx.Hash())
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "the %s transaction is not in the pool", name)
	gomega.Expect(isPending).To(gomega.BeTrue(), "the %s transaction was mined", name)
}

// expectReplaced fails if the node still has the transaction.
func expectReplaced(ctx context.Context, node *live.Node, tx *types.Transaction, name string) {
	ginkgo.GinkgoHelper()
	_, _, err := node.Execution.TransactionByHash(ctx, tx.Hash())
	gomega.Expect(err).To(gomega.MatchError(qrl.NotFound), "the %s transaction is still in the pool", name)
}

// expectNotMined fails if the transaction has a receipt.
func expectNotMined(ctx context.Context, node *live.Node, tx *types.Transaction, name string) {
	ginkgo.GinkgoHelper()
	_, err := node.Execution.TransactionReceipt(ctx, tx.Hash())
	gomega.Expect(err).To(gomega.MatchError(qrl.NotFound), "the %s transaction was mined", name)
}

// waitForBlock returns once the chain has a new head.
func waitForBlock(ctx context.Context, node *live.Node) {
	ginkgo.GinkgoHelper()
	head := testsuite.MustSucceed(node.Execution.BlockNumber(ctx))
	gomega.Eventually(func(g gomega.Gomega) {
		g.Expect(node.Execution.BlockNumber(ctx)).To(gomega.BeNumerically(">", head))
	}).WithContext(ctx).
		WithTimeout(time.Minute).
		WithPolling(pollInterval).
		Should(gomega.Succeed())
}
