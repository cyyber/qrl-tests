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

	// Pool error. core/txpool does not build with this module's dependencies.
	replaceUnderpriced = "replacement transaction underpriced"

	// pollInterval is short next to a slot, so a spec acts soon after a block
	// lands or the pool promotes a transaction.
	pollInterval = 200 * time.Millisecond
	// stuckAfterBlocks is how many blocks the specs wait before they check
	// that a low-fee transaction is still unmined. A transaction with normal
	// fees is mined within two blocks, so one left out of three is stuck, not
	// slow.
	stuckAfterBlocks = 3

	// The stuck transaction's fee cap has to stay below the base fee until the
	// specs have replaced it. On an idle chain the base fee drops by 1/8 with
	// every block, so a cap just under it would be above it a block later,
	// and the transaction would be mined.
	//
	// stuckFeeCapDivisor makes the cap 1/32 of the base fee. The base fee
	// takes about 25 blocks to drop that far, and the suite takes about 10.
	stuckFeeCapDivisor = 32
	// minStuckFeeCap is the cap, in planck, when 1/32 of the base fee is
	// lower. That is the case on a chain that has been idle for long: its
	// base fee has dropped to 7 planck and stays there, because 1/8 of 7
	// rounds down to zero. 4 planck is below 7, and so is the 5 planck that a
	// spec raises the cap to.
	minStuckFeeCap = 4
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Transaction pool E2E suite")
}

// feeCase names the fees of a replacement that the pool rejects.
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

			// The stuck fees are below the base fee, and the raised fees
			// cover it.
			stuckFeeCap, stuckTip   *big.Int
			raisedFeeCap, raisedTip *big.Int
			stuck, bumped           *types.Transaction

			stuckRecipient       = common.BytesToAddress([]byte("txpool e2e stuck"))
			replacementRecipient = common.BytesToAddress([]byte("txpool e2e replacement"))
			mistakenRecipient    = common.BytesToAddress([]byte("txpool e2e mistaken"))
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
		// replacement has the stuck transaction's sender and nonce, and another
		// recipient.
		replacement := func(gasFeeCap, gasTipCap *big.Int) *types.Transaction {
			ginkgo.GinkgoHelper()
			return transfer(sender, 0, replacementRecipient, big.NewInt(1), gasFeeCap, gasTipCap)
		}

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			node = testsuite.MustSucceed(testsuite.LoadRuntime().PrimaryNode(ctx))
			signer = types.LatestSignerForChainID(node.ChainID)
			head := testsuite.MustSucceed(node.Execution.HeaderByNumber(ctx, nil))
			tip := testsuite.MustSucceed(node.Execution.SuggestGasTipCap(ctx))
			two := big.NewInt(2)

			stuckFeeCap = new(big.Int).Div(head.BaseFee, big.NewInt(stuckFeeCapDivisor))
			if stuckFeeCap.Cmp(big.NewInt(minStuckFeeCap)) < 0 {
				stuckFeeCap.SetInt64(minStuckFeeCap)
			}
			gomega.Expect(bump(stuckFeeCap).Cmp(head.BaseFee)).To(gomega.BeNumerically("<", 0),
				"the base fee %s leaves no room for a stuck fee cap", head.BaseFee)
			// Half the fee cap, so a bumped tip stays below the fee cap and the
			// replacement is rejected as underpriced, not as invalid.
			stuckTip = new(big.Int).Div(stuckFeeCap, two)

			// The raised tip is the suggested one, unless the price bump over
			// the bumped tip asks for more. The fee cap has room for the base
			// fee to double.
			raisedTip = bump(bump(stuckTip))
			if raisedTip.Cmp(tip) < 0 {
				raisedTip = tip
			}
			raisedFeeCap = new(big.Int).Mul(head.BaseFee, two)
			raisedFeeCap.Add(raisedFeeCap, raisedTip)

			// A stuck transaction blocks its sender's later nonces, so a new
			// account sends it and the shared wallet stays usable if a spec
			// fails.
			sender = testsuite.MustSucceed(qrlwallet.Generate(qrlwallet.ML_DSA_87))
			senderAddress = common.Address(sender.GetAddress())
			// The sender has four transactions mined. Each can cost its gas at
			// the raised fee cap after a price bump, plus a value of one planck.
			funding := new(big.Int).Mul(new(big.Int).SetUint64(params.TxGas), bump(raisedFeeCap))
			funding.Add(funding, big.NewInt(1))
			funding.Mul(funding, big.NewInt(4))
			walletNonce := testsuite.MustSucceed(node.Execution.PendingNonceAt(ctx, node.Address))
			fund := transfer(node.Wallet, walletNonce, senderAddress, funding, raisedFeeCap, raisedTip)
			gomega.Expect(node.Execution.SendTransaction(ctx, fund)).To(gomega.Succeed())
			testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, fund))
		}, ginkgo.NodeTimeout(2*time.Minute))

		// This simulates congestion, which leaves a transaction stuck by
		// raising the base fee above its fee cap. The transaction starts with
		// a fee cap below the base fee instead, which the pool treats the same.
		ginkgo.It("leaves a transaction with a fee cap below the base fee out of blocks", func(ctx ginkgo.SpecContext) {
			stuck = transfer(sender, 0, stuckRecipient, big.NewInt(1), stuckFeeCap, stuckTip)
			// The pool accepts it once it has processed the block that funds
			// the sender.
			gomega.Eventually(func() error {
				return node.Execution.SendTransaction(ctx, stuck)
			}).WithContext(ctx).
				WithTimeout(time.Minute).
				WithPolling(pollInterval).
				Should(gomega.Succeed())

			// The pending nonce moves: the transaction has the sender's next
			// nonce, and only its fees keep it out of a block.
			gomega.Eventually(func(g gomega.Gomega) {
				g.Expect(node.Execution.PendingNonceAt(ctx, senderAddress)).To(gomega.Equal(uint64(1)))
			}).WithContext(ctx).
				WithTimeout(time.Minute).
				WithPolling(pollInterval).
				Should(gomega.Succeed())

			for range stuckAfterBlocks {
				waitForBlock(ctx, node)
			}
			expectPooled(ctx, node, stuck, "stuck")
		}, ginkgo.SpecTimeout(3*time.Minute))

		// Below 10 planck the price bump rounds down to nothing and the pool
		// only asks for a higher fee. On a chain whose base fee has settled,
		// the stuck fees are that low, and the "one planck short" cases repeat
		// the two before them. The last spec has them with fees that are
		// always large enough.
		ginkgo.It("rejects a replacement that does not raise both fees by the price bump", func(ctx ginkgo.SpecContext) {
			for _, fees := range []feeCase{
				{"the same fees", stuckFeeCap, stuckTip},
				{"only the fee cap raised", bump(stuckFeeCap), stuckTip},
				{"only the tip raised", stuckFeeCap, bump(stuckTip)},
				{"the fee cap one planck short of the price bump", belowBump(stuckFeeCap), bump(stuckTip)},
				{"the tip one planck short of the price bump", bump(stuckFeeCap), belowBump(stuckTip)},
			} {
				err := node.Execution.SendTransaction(ctx, replacement(fees.feeCap, fees.tip))
				gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring(replaceUnderpriced)),
					"replacement with %s", fees.name)
			}

			expectPooled(ctx, node, stuck, "stuck")
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("accepts a replacement that raises both fees by the price bump", func(ctx ginkgo.SpecContext) {
			bumped = replacement(bump(stuckFeeCap), bump(stuckTip))
			gomega.Expect(node.Execution.SendTransaction(ctx, bumped)).To(gomega.Succeed())

			expectReplaced(ctx, node, stuck, "stuck")
			expectPooled(ctx, node, bumped, "bumped")
		}, ginkgo.SpecTimeout(time.Minute))

		// The bumped fee cap is still below the base fee, so the replacement is
		// as stuck as the transaction it replaced. The sender sends the stuck
		// transaction's payment again, with fees that cover the base fee.
		ginkgo.It("mines the payment once its fees cover the base fee", func(ctx ginkgo.SpecContext) {
			before := testsuite.MustSucceed(node.Execution.BalanceAt(ctx, stuckRecipient, nil))
			raised := transfer(sender, 0, stuckRecipient, big.NewInt(1), raisedFeeCap, raisedTip)
			gomega.Expect(node.Execution.SendTransaction(ctx, raised)).To(gomega.Succeed())

			receipt := testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, raised))
			gomega.Expect(receipt.Status).To(gomega.Equal(types.ReceiptStatusSuccessful))

			expectUnmined(ctx, node, stuck, "stuck")
			expectUnmined(ctx, node, bumped, "bumped")
			gomega.Expect(testsuite.MustSucceed(node.Execution.BalanceAt(ctx, stuckRecipient, receipt.BlockNumber))).To(
				gomega.Equal(new(big.Int).Add(before, big.NewInt(1))))
		}, ginkgo.SpecTimeout(2*time.Minute))

		// A transaction sent by mistake is cancelled the same way: a transfer of
		// nothing to its own sender takes its nonce, so it can never be mined.
		// The mistaken transaction has the stuck fees, so no block includes it
		// before it is replaced.
		ginkgo.It("cancels a stuck transaction with a zero-value transfer to its sender", func(ctx ginkgo.SpecContext) {
			nonce := testsuite.MustSucceed(node.Execution.PendingNonceAt(ctx, senderAddress))
			before := testsuite.MustSucceed(node.Execution.BalanceAt(ctx, mistakenRecipient, nil))
			mistaken := transfer(sender, nonce, mistakenRecipient, big.NewInt(1), stuckFeeCap, stuckTip)
			gomega.Expect(node.Execution.SendTransaction(ctx, mistaken)).To(gomega.Succeed())
			expectPooled(ctx, node, mistaken, "mistaken")

			cancel := transfer(sender, nonce, senderAddress, new(big.Int), raisedFeeCap, raisedTip)
			gomega.Expect(node.Execution.SendTransaction(ctx, cancel)).To(gomega.Succeed())
			expectReplaced(ctx, node, mistaken, "mistaken")

			receipt := testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, cancel))
			gomega.Expect(receipt.Status).To(gomega.Equal(types.ReceiptStatusSuccessful))

			expectUnmined(ctx, node, mistaken, "mistaken")
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
			mistaken := transfer(sender, nonce+1, mistakenRecipient, big.NewInt(1), raisedFeeCap, raisedTip)
			gomega.Expect(node.Execution.SendTransaction(ctx, mistaken)).To(gomega.Succeed())
			expectPooled(ctx, node, mistaken, "mistaken")
			// The pending nonce does not move: the transaction is queued.
			gomega.Expect(node.Execution.PendingNonceAt(ctx, senderAddress)).To(gomega.Equal(nonce))

			// The raised fees are far above 10 planck, so the "one planck short"
			// cases pin the price bump whatever the base fee is.
			for _, fees := range []feeCase{
				{"the same fees", raisedFeeCap, raisedTip},
				{"the fee cap one planck short of the price bump", belowBump(raisedFeeCap), bump(raisedTip)},
				{"the tip one planck short of the price bump", bump(raisedFeeCap), belowBump(raisedTip)},
			} {
				err := node.Execution.SendTransaction(ctx,
					transfer(sender, nonce+1, senderAddress, new(big.Int), fees.feeCap, fees.tip))
				gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring(replaceUnderpriced)),
					"cancelling transfer with %s", fees.name)
			}

			cancel := transfer(sender, nonce+1, senderAddress, new(big.Int), bump(raisedFeeCap), bump(raisedTip))
			gomega.Expect(node.Execution.SendTransaction(ctx, cancel)).To(gomega.Succeed())
			expectReplaced(ctx, node, mistaken, "mistaken")

			ginkgo.By("filling the nonce gap")
			filler := transfer(sender, nonce, senderAddress, new(big.Int), raisedFeeCap, raisedTip)
			gomega.Expect(node.Execution.SendTransaction(ctx, filler)).To(gomega.Succeed())

			receipt := testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, cancel))
			gomega.Expect(receipt.Status).To(gomega.Equal(types.ReceiptStatusSuccessful))

			expectUnmined(ctx, node, mistaken, "mistaken")
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

// expectPooled fails unless the pool holds the transaction, unmined.
func expectPooled(ctx context.Context, node *live.Node, tx *types.Transaction, name string) {
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

// expectUnmined fails if the transaction has a receipt.
func expectUnmined(ctx context.Context, node *live.Node, tx *types.Transaction, name string) {
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
