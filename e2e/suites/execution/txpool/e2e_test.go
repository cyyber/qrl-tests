//go:build e2e

package txpool

import (
	"math/big"
	"strings"
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
	"github.com/theQRL/go-qrl/params"
)

const (
	// priceBump is the pool's default --txpool.pricebump, in percent.
	priceBump = 10

	// Pool errors. core/txpool does not build with this module's dependencies.
	replaceUnderpriced = "replacement transaction underpriced"
	nonceTooLow        = "nonce too low"

	pollInterval = time.Second
	// replaceAttempts limits the tries to replace an executable transaction before it is mined.
	replaceAttempts = 5
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
			node        *live.Node
			signer      types.Signer
			nonce       uint64
			feeCap, tip *big.Int
			original    *types.Transaction
			replacement *types.Transaction

			originalRecipient    = common.BytesToAddress([]byte("txpool e2e original"))
			replacementRecipient = common.BytesToAddress([]byte("txpool e2e replacement"))
		)

		transfer := func(txNonce uint64, to common.Address, gasFeeCap, gasTipCap *big.Int) *types.Transaction {
			ginkgo.GinkgoHelper()
			return testsuite.MustSucceed(types.SignNewTx(node.Wallet, signer, &types.DynamicFeeTx{
				ChainID:   node.ChainID,
				Nonce:     txNonce,
				GasTipCap: gasTipCap,
				GasFeeCap: gasFeeCap,
				Gas:       params.TxGas,
				To:        &to,
				Value:     big.NewInt(1),
			}))
		}

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			node = testsuite.MustSucceed(testsuite.LoadRuntime().PrimaryNode(ctx))
			signer = types.LatestSignerForChainID(node.ChainID)
			nonce = testsuite.MustSucceed(node.Execution.PendingNonceAt(ctx, node.Address))

			head := testsuite.MustSucceed(node.Execution.HeaderByNumber(ctx, nil))
			tip = testsuite.MustSucceed(node.Execution.SuggestGasTipCap(ctx))
			two := big.NewInt(2)
			// Twice the tip, so a bumped tip stays below the fee cap and the
			// replacement is rejected as underpriced, not as invalid.
			feeCap = new(big.Int).Add(new(big.Int).Mul(head.BaseFee, two), new(big.Int).Mul(tip, two))
		}, ginkgo.NodeTimeout(time.Minute))

		// The next nonce is left free, so the transaction waits in the pool
		// instead of being included in the next block.
		ginkgo.It("keeps a transaction with a future nonce in the pool", func(ctx ginkgo.SpecContext) {
			original = transfer(nonce+1, originalRecipient, feeCap, tip)
			gomega.Expect(node.Execution.SendTransaction(ctx, original)).To(gomega.Succeed())

			_, _, err := node.Execution.TransactionByHash(ctx, original.Hash())
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "the transaction is not in the pool")
			// The pending nonce does not move: the transaction is queued.
			gomega.Expect(node.Execution.PendingNonceAt(ctx, node.Address)).To(gomega.Equal(nonce))
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("rejects a replacement that does not raise both fees by the price bump", func(ctx ginkgo.SpecContext) {
			for _, fees := range []struct {
				name        string
				feeCap, tip *big.Int
			}{
				{"the same fees", feeCap, tip},
				{"only the fee cap raised", bump(feeCap), tip},
				{"only the tip raised", feeCap, bump(tip)},
				{"the fee cap one planck short of the price bump", belowBump(feeCap), bump(tip)},
				{"the tip one planck short of the price bump", bump(feeCap), belowBump(tip)},
			} {
				err := node.Execution.SendTransaction(ctx, transfer(nonce+1, replacementRecipient, fees.feeCap, fees.tip))
				gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring(replaceUnderpriced)),
					"replacement with %s", fees.name)
			}

			_, _, err := node.Execution.TransactionByHash(ctx, original.Hash())
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "the original transaction left the pool")
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("accepts a replacement that raises both fees by the price bump", func(ctx ginkgo.SpecContext) {
			replacement = transfer(nonce+1, replacementRecipient, bump(feeCap), bump(tip))
			gomega.Expect(node.Execution.SendTransaction(ctx, replacement)).To(gomega.Succeed())

			_, _, err := node.Execution.TransactionByHash(ctx, original.Hash())
			gomega.Expect(err).To(gomega.MatchError(qrl.NotFound), "the original transaction is still in the pool")
			_, _, err = node.Execution.TransactionByHash(ctx, replacement.Hash())
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "the replacement is not in the pool")
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("mines the replacement and not the original", func(ctx ginkgo.SpecContext) {
			originalBefore := testsuite.MustSucceed(node.Execution.BalanceAt(ctx, originalRecipient, nil))
			replacementBefore := testsuite.MustSucceed(node.Execution.BalanceAt(ctx, replacementRecipient, nil))

			ginkgo.By("filling the nonce gap")
			filler := transfer(nonce, node.Address, feeCap, tip)
			gomega.Expect(node.Execution.SendTransaction(ctx, filler)).To(gomega.Succeed())
			testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, filler))

			receipt := testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, replacement))
			gomega.Expect(receipt.Status).To(gomega.Equal(types.ReceiptStatusSuccessful))

			_, err := node.Execution.TransactionReceipt(ctx, original.Hash())
			gomega.Expect(err).To(gomega.MatchError(qrl.NotFound), "the original transaction was mined")

			mined := receipt.BlockNumber
			gomega.Expect(testsuite.MustSucceed(node.Execution.NonceAt(ctx, node.Address, mined))).To(gomega.Equal(nonce + 2))
			gomega.Expect(testsuite.MustSucceed(node.Execution.BalanceAt(ctx, originalRecipient, mined))).To(
				gomega.Equal(originalBefore))
			gomega.Expect(testsuite.MustSucceed(node.Execution.BalanceAt(ctx, replacementRecipient, mined))).To(
				gomega.Equal(new(big.Int).Add(replacementBefore, big.NewInt(1))))
		}, ginkgo.SpecTimeout(2*time.Minute))

		// A transaction with the next nonce is executable, and the pool replaces
		// it on a different path than a queued one. The next block also mines
		// it, so the spec starts over when that happens before the replacement.
		ginkgo.It("replaces an executable transaction before it is mined", func(ctx ginkgo.SpecContext) {
			for range replaceAttempts {
				next := testsuite.MustSucceed(node.Execution.PendingNonceAt(ctx, node.Address))
				executable := transfer(next, originalRecipient, feeCap, tip)
				gomega.Expect(node.Execution.SendTransaction(ctx, executable)).To(gomega.Succeed())

				// The pending nonce moves once the pool has promoted the
				// transaction from its queue.
				gomega.Eventually(func(g gomega.Gomega) {
					g.Expect(node.Execution.PendingNonceAt(ctx, node.Address)).To(gomega.Equal(next + 1))
				}).WithContext(ctx).
					WithTimeout(time.Minute).
					WithPolling(pollInterval).
					Should(gomega.Succeed())

				err := node.Execution.SendTransaction(ctx, transfer(next, replacementRecipient, feeCap, tip))
				if staleNonce(err) {
					continue
				}
				gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring(replaceUnderpriced)),
					"replacement with the same fees")

				bumped := transfer(next, replacementRecipient, bump(feeCap), bump(tip))
				err = node.Execution.SendTransaction(ctx, bumped)
				if staleNonce(err) {
					continue
				}
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				gomega.Eventually(func(g gomega.Gomega) {
					g.Expect(node.Execution.NonceAt(ctx, node.Address, nil)).To(gomega.BeNumerically(">", next))
				}).WithContext(ctx).
					WithTimeout(time.Minute).
					WithPolling(pollInterval).
					Should(gomega.Succeed())

				// A block that was already being built can still include the
				// original.
				_, err = node.Execution.TransactionReceipt(ctx, executable.Hash())
				if err == nil {
					continue
				}
				gomega.Expect(err).To(gomega.MatchError(qrl.NotFound))
				receipt := testsuite.MustSucceed(node.Execution.TransactionReceipt(ctx, bumped.Hash()))
				gomega.Expect(receipt.Status).To(gomega.Equal(types.ReceiptStatusSuccessful))
				return
			}
			ginkgo.Fail("the original transaction was mined before its replacement in every attempt")
		}, ginkgo.SpecTimeout(5*time.Minute))
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

// staleNonce reports whether the pool rejected a transaction because its nonce
// was already used.
func staleNonce(err error) bool {
	return err != nil && strings.Contains(err.Error(), nonceTooLow)
}

// belowBump raises fee by one planck less than the price bump.
func belowBump(fee *big.Int) *big.Int {
	return new(big.Int).Sub(bump(fee), big.NewInt(1))
}
