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
	// stuckBlocks is how many blocks must leave a transaction out before a
	// spec calls it stuck. One that pays the base fee is mined within two.
	stuckBlocks = 3

	// stuckFeeFraction divides the base fee into the stuck fee cap. The base
	// fee falls by an eighth per empty block, so the cap stays below it for
	// 25 blocks, even after a price bump.
	stuckFeeFraction = 32
	// minStuckFeeCap is the lowest stuck fee cap, in planck. The base fee
	// stops falling at 7, which is above this cap after a price bump.
	minStuckFeeCap = 4
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Transaction pool E2E suite")
}

// A congested network raises the base fee above a waiting transaction's fee
// cap, and no block can include the transaction until its sender raises the
// fees. The devnet cannot be congested on demand, so the specs send a
// transaction whose fee cap starts below the base fee and then replace it.
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

			stuckFeeCap = new(big.Int).Div(head.BaseFee, big.NewInt(stuckFeeFraction))
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
			// The raised transaction can cost its gas at the fee cap, plus its
			// value.
			funding := new(big.Int).Mul(new(big.Int).SetUint64(params.TxGas), raisedFeeCap)
			funding.Add(funding, big.NewInt(1))
			walletNonce := testsuite.MustSucceed(node.Execution.PendingNonceAt(ctx, node.Address))
			fund := transfer(node.Wallet, walletNonce, senderAddress, funding, raisedFeeCap, raisedTip)
			gomega.Expect(node.Execution.SendTransaction(ctx, fund)).To(gomega.Succeed())
			testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, fund))
		}, ginkgo.NodeTimeout(2*time.Minute))

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

			for range stuckBlocks {
				waitForBlock(ctx, node)
			}
			_, isPending, err := node.Execution.TransactionByHash(ctx, stuck.Hash())
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "the stuck transaction left the pool")
			gomega.Expect(isPending).To(gomega.BeTrue(), "the stuck transaction was mined")
		}, ginkgo.SpecTimeout(3*time.Minute))

		// Below 10 planck the price bump rounds down to nothing and the pool
		// only asks for a higher fee. On a chain whose base fee has settled,
		// the stuck fees are that low, and the "one planck short" cases repeat
		// the two before them.
		ginkgo.It("rejects a replacement that does not raise both fees by the price bump", func(ctx ginkgo.SpecContext) {
			for _, fees := range []struct {
				name        string
				feeCap, tip *big.Int
			}{
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

			_, isPending, err := node.Execution.TransactionByHash(ctx, stuck.Hash())
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "the stuck transaction left the pool")
			gomega.Expect(isPending).To(gomega.BeTrue(), "the stuck transaction was mined")
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("accepts a replacement that raises both fees by the price bump", func(ctx ginkgo.SpecContext) {
			bumped = replacement(bump(stuckFeeCap), bump(stuckTip))
			gomega.Expect(node.Execution.SendTransaction(ctx, bumped)).To(gomega.Succeed())

			_, _, err := node.Execution.TransactionByHash(ctx, stuck.Hash())
			gomega.Expect(err).To(gomega.MatchError(qrl.NotFound), "the stuck transaction is still in the pool")
			_, isPending, err := node.Execution.TransactionByHash(ctx, bumped.Hash())
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "the replacement is not in the pool")
			gomega.Expect(isPending).To(gomega.BeTrue(), "the replacement was mined")
		}, ginkgo.SpecTimeout(time.Minute))

		// The bumped fee cap is still below the base fee, so the replacement is
		// as stuck as the transaction it replaced.
		ginkgo.It("mines a replacement whose fees cover the base fee", func(ctx ginkgo.SpecContext) {
			raised := replacement(raisedFeeCap, raisedTip)
			gomega.Expect(node.Execution.SendTransaction(ctx, raised)).To(gomega.Succeed())

			receipt := testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, raised))
			gomega.Expect(receipt.Status).To(gomega.Equal(types.ReceiptStatusSuccessful))

			_, err := node.Execution.TransactionReceipt(ctx, stuck.Hash())
			gomega.Expect(err).To(gomega.MatchError(qrl.NotFound), "the stuck transaction was mined")
			_, err = node.Execution.TransactionReceipt(ctx, bumped.Hash())
			gomega.Expect(err).To(gomega.MatchError(qrl.NotFound), "the bumped transaction was mined")
			gomega.Expect(testsuite.MustSucceed(node.Execution.NonceAt(ctx, senderAddress, receipt.BlockNumber))).To(
				gomega.Equal(uint64(1)))
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
