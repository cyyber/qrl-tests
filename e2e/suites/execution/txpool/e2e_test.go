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

			recipient = common.BytesToAddress([]byte("txpool e2e recipient"))
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
			for _, fees := range []struct {
				name        string
				feeCap, tip *big.Int
			}{
				{"the same fees", stuckFeeCap, stuckTip},
				{"only the fee cap raised", bump(stuckFeeCap), stuckTip},
				{"only the tip raised", stuckFeeCap, bump(stuckTip)},
			} {
				// Another recipient, so the same fees do not make the same
				// transaction.
				replacement := payment(0, senderAddress, fees.feeCap, fees.tip)
				err := node.Execution.SendTransaction(ctx, replacement)
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

// expectInPool fails unless the transaction is in the pool, not yet mined.
func expectInPool(ctx context.Context, node *live.Node, tx *types.Transaction, name string) {
	ginkgo.GinkgoHelper()
	_, isPending, err := node.Execution.TransactionByHash(ctx, tx.Hash())
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "the %s transaction is not in the pool", name)
	gomega.Expect(isPending).To(gomega.BeTrue(), "the %s transaction was mined", name)
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
