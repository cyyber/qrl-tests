//go:build e2e

package rpc

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
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
	qrlwallet "github.com/theQRL/go-qrl/crypto/pqcrypto/wallet"
	"github.com/theQRL/go-qrl/params"
	gorpc "github.com/theQRL/go-qrl/rpc"
)

// Fees of a transaction that stays in the pool, in planck. The base fee never
// drops below 7, so no block includes it.
var (
	pendingFeeCap = big.NewInt(4)
	pendingTip    = big.NewInt(2)
)

const (
	// finalityTimeout bounds the wait for the first finalized checkpoint,
	// a few 40-second epochs after genesis.
	finalityTimeout = 5 * time.Minute
	pollInterval    = time.Second
)

// Header fields Zond has from genesis, which the API always returns.
var headerFields = []string{
	"parentHash", "miner", "stateRoot", "transactionsRoot", "receiptsRoot", "logsBloom",
	"number", "gasLimit", "gasUsed", "timestamp", "extraData", "prevRandao",
	"baseFeePerGas", "withdrawalsRoot",
}

// Transaction fields the API always returns, mined or pending.
var transactionFields = []string{"type", "chainId", "nonce", "gas", "maxFeePerGas", "maxPriorityFeePerGas", "accessList", "value", "input"}

// blockTag is one way of naming a block: a JSON-RPC tag, a number or a hash.
type blockTag struct {
	name    string
	arg     func() any         // the JSON-RPC block argument
	number  func() *big.Int    // the qrlclient block number; nil for a hash
	hash    func() common.Hash // the block hash, for a hash tag
	ref     func() gorpc.BlockNumberOrHash
	pending bool
}

func TestE2E(t *testing.T) {
	testsuite.Run(t, "JSON-RPC E2E suite")
}

var _ = ginkgo.Describe(
	"JSON-RPC blocks, transactions and receipts against a live qrl-package network",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "rpc"),
	func() {
		var (
			node    *live.Node
			mined   *types.Transaction
			receipt *types.Receipt
			pending *types.Transaction

			recipient = common.BytesToAddress([]byte("rpc e2e recipient"))
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			node = testsuite.MustSucceed(testsuite.LoadRuntime().PrimaryNode(ctx))
			signer := types.NewZondSigner(node.ChainID)
			head := testsuite.MustSucceed(node.Execution.HeaderByNumber(ctx, nil))
			tip := testsuite.MustSucceed(node.Execution.SuggestGasTipCap(ctx))
			// Room for the base fee to double.
			feeCap := new(big.Int).Mul(head.BaseFee, big.NewInt(2))
			feeCap.Add(feeCap, tip)

			ginkgo.By("funding a new account, which mines a transaction")
			// The pending transaction blocks its sender's later nonces, so the
			// shared wallet does not send it.
			sender := testsuite.MustSucceed(qrlwallet.Generate(qrlwallet.ML_DSA_87))
			senderAddress := common.Address(sender.GetAddress())
			nonce := testsuite.MustSucceed(node.Execution.PendingNonceAt(ctx, node.Address))
			mined = testsuite.MustSucceed(types.SignNewTx(node.Wallet, signer, &types.DynamicFeeTx{
				ChainID:   node.ChainID,
				Nonce:     nonce,
				GasTipCap: tip,
				GasFeeCap: feeCap,
				Gas:       params.TxGas,
				To:        &senderAddress,
				Value:     big.NewInt(params.Quanta),
			}))
			gomega.Expect(node.Execution.SendTransaction(ctx, mined)).To(gomega.Succeed())
			receipt = testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, mined))

			ginkgo.By("sending a transaction that stays in the pool")
			pending = testsuite.MustSucceed(types.SignNewTx(sender, signer, &types.DynamicFeeTx{
				ChainID:   node.ChainID,
				Nonce:     0,
				GasTipCap: pendingTip,
				GasFeeCap: pendingFeeCap,
				Gas:       params.TxGas,
				To:        &recipient,
				Value:     big.NewInt(1),
			}))
			gomega.Expect(node.Execution.SendTransaction(ctx, pending)).To(gomega.Succeed())

			ginkgo.By("waiting for a finalized block")
			gomega.Eventually(func() error {
				_, err := node.Execution.HeaderByNumber(ctx, big.NewInt(int64(gorpc.FinalizedBlockNumber)))
				return err
			}).WithContext(ctx).WithTimeout(finalityTimeout).WithPolling(pollInterval).Should(gomega.Succeed())
		}, ginkgo.NodeTimeout(finalityTimeout+2*time.Minute))

		named := func(name string, number gorpc.BlockNumber) blockTag {
			return blockTag{
				name:    name,
				arg:     func() any { return number.String() },
				number:  func() *big.Int { return big.NewInt(int64(number)) },
				ref:     func() gorpc.BlockNumberOrHash { return gorpc.BlockNumberOrHashWithNumber(number) },
				pending: number == gorpc.PendingBlockNumber,
			}
		}
		tags := []blockTag{
			{
				name:   "the genesis block by number",
				arg:    func() any { return "0x0" },
				number: func() *big.Int { return new(big.Int) },
				ref:    func() gorpc.BlockNumberOrHash { return gorpc.BlockNumberOrHashWithNumber(0) },
			},
			named("the earliest block", gorpc.EarliestBlockNumber),
			{
				name:   "a block by number",
				arg:    func() any { return hexutil.EncodeBig(receipt.BlockNumber) },
				number: func() *big.Int { return receipt.BlockNumber },
				ref: func() gorpc.BlockNumberOrHash {
					return gorpc.BlockNumberOrHashWithNumber(gorpc.BlockNumber(receipt.BlockNumber.Int64()))
				},
			},
			{
				name: "a block by hash",
				arg:  func() any { return receipt.BlockHash },
				hash: func() common.Hash { return receipt.BlockHash },
				ref:  func() gorpc.BlockNumberOrHash { return gorpc.BlockNumberOrHashWithHash(receipt.BlockHash, false) },
			},
			named("the latest block", gorpc.LatestBlockNumber),
			named("the safe block", gorpc.SafeBlockNumber),
			named("the finalized block", gorpc.FinalizedBlockNumber),
			named("the pending block", gorpc.PendingBlockNumber),
		}

		for _, tag := range tags {
			ginkgo.It("decodes the header and block of "+tag.name+" with every field Zond has from genesis", func(ctx ginkgo.SpecContext) {
				var (
					header *types.Header
					block  *types.Block
					err    error
				)
				if tag.hash != nil {
					header, err = node.Execution.HeaderByHash(ctx, tag.hash())
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					block, err = node.Execution.BlockByHash(ctx, tag.hash())
				} else {
					header, err = node.Execution.HeaderByNumber(ctx, tag.number())
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					block, err = node.Execution.BlockByNumber(ctx, tag.number())
				}
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(header.BaseFee).NotTo(gomega.BeNil())
				gomega.Expect(block.BaseFee()).NotTo(gomega.BeNil())
				gomega.Expect(block.Withdrawals()).NotTo(gomega.BeNil())
				if !tag.pending {
					// A tag can move between the two calls, but not past the
					// header the first call returned.
					gomega.Expect(block.NumberU64()).To(gomega.BeNumerically(">=", header.Number.Uint64()))
				}

				headerMethod, blockMethod := "qrl_getHeaderByNumber", "qrl_getBlockByNumber"
				if tag.hash != nil {
					headerMethod, blockMethod = "qrl_getHeaderByHash", "qrl_getBlockByHash"
				}
				for _, fullTx := range []bool{false, true} {
					raw := callObject(ctx, node, blockMethod, tag.arg(), fullTx)
					expectHeaderFields(raw, tag.pending)
					gomega.Expect(raw["withdrawals"]).To(gomega.BeAssignableToTypeOf([]any{}))
					gomega.Expect(raw["transactions"]).To(gomega.BeAssignableToTypeOf([]any{}))
					if fullTx {
						for _, tx := range raw["transactions"].([]any) {
							expectFields(tx.(map[string]any), transactionFields...)
						}
					}
				}
				expectHeaderFields(callObject(ctx, node, headerMethod, tag.arg()), tag.pending)
			})

			if !tag.pending {
				// Receipts are only served for blocks in the database.
				ginkgo.It("decodes the receipts of "+tag.name+" with their type, status and effective gas price", func(ctx ginkgo.SpecContext) {
					receipts, err := node.Execution.BlockReceipts(ctx, tag.ref())
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					for _, receipt := range receipts {
						gomega.Expect(receipt.Type).To(gomega.Equal(uint8(types.DynamicFeeTxType)))
						gomega.Expect(receipt.EffectiveGasPrice).NotTo(gomega.BeNil())
					}
					var raw []map[string]any
					gomega.Expect(node.Execution.Client().CallContext(ctx, &raw, "qrl_getBlockReceipts", tag.arg())).To(gomega.Succeed())
					for _, receipt := range raw {
						expectFields(receipt, "type", "status", "effectiveGasPrice", "blockHash", "blockNumber")
					}
				})
			}
		}

		ginkgo.It("decodes a mined transaction and its receipt by hash and by block position", func(ctx ginkgo.SpecContext) {
			tx, isPending, err := node.Execution.TransactionByHash(ctx, mined.Hash())
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(isPending).To(gomega.BeFalse())
			gomega.Expect(tx.Hash()).To(gomega.Equal(mined.Hash()))
			gomega.Expect(tx.AccessList()).NotTo(gomega.BeNil())
			gomega.Expect(tx.ChainId()).To(gomega.Equal(node.ChainID))

			index := uint(receipt.TransactionIndex)
			inBlock := testsuite.MustSucceed(node.Execution.TransactionInBlock(ctx, receipt.BlockHash, index))
			gomega.Expect(inBlock.Hash()).To(gomega.Equal(mined.Hash()))

			gomega.Expect(receipt.Type).To(gomega.Equal(uint8(types.DynamicFeeTxType)))
			gomega.Expect(receipt.Status).To(gomega.Equal(types.ReceiptStatusSuccessful))
			block := testsuite.MustSucceed(node.Execution.BlockByHash(ctx, receipt.BlockHash))
			wantPrice := new(big.Int).Add(mined.GasTipCap(), block.BaseFee())
			if wantPrice.Cmp(mined.GasFeeCap()) > 0 {
				wantPrice = mined.GasFeeCap()
			}
			gomega.Expect(receipt.EffectiveGasPrice).To(gomega.Equal(wantPrice))

			for _, raw := range []map[string]any{
				callObject(ctx, node, "qrl_getTransactionByHash", mined.Hash()),
				callObject(ctx, node, "qrl_getTransactionByBlockHashAndIndex", receipt.BlockHash, hexutil.Uint(index)),
				callObject(ctx, node, "qrl_getTransactionByBlockNumberAndIndex", hexutil.EncodeBig(receipt.BlockNumber), hexutil.Uint(index)),
			} {
				expectFields(raw, transactionFields...)
				expectFields(raw, "hash", "blockHash", "blockNumber", "transactionIndex", "gasPrice")
				gomega.Expect(raw).To(gomega.HaveKeyWithValue("gasPrice", hexutil.EncodeBig(wantPrice)))
			}
			expectFields(callObject(ctx, node, "qrl_getTransactionReceipt", mined.Hash()), "type", "status", "effectiveGasPrice")
		})

		ginkgo.It("decodes a pending transaction, which has no block or receipt yet", func(ctx ginkgo.SpecContext) {
			tx, isPending, err := node.Execution.TransactionByHash(ctx, pending.Hash())
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(isPending).To(gomega.BeTrue())
			gomega.Expect(tx.Hash()).To(gomega.Equal(pending.Hash()))
			gomega.Expect(tx.AccessList()).NotTo(gomega.BeNil())

			_, err = node.Execution.TransactionReceipt(ctx, pending.Hash())
			gomega.Expect(err).To(gomega.MatchError(qrl.NotFound))

			raw := callObject(ctx, node, "qrl_getTransactionByHash", pending.Hash())
			expectFields(raw, transactionFields...)
			gomega.Expect(raw).To(gomega.HaveKeyWithValue("gasPrice", hexutil.EncodeBig(pendingFeeCap)))
			for _, field := range []string{"blockHash", "blockNumber", "transactionIndex"} {
				gomega.Expect(raw).To(gomega.HaveKeyWithValue(field, gomega.BeNil()), "field %q", field)
			}
		})

		for _, last := range []struct {
			name   string
			number *big.Int
		}{
			{"the latest block", nil},
			{"the pending block", big.NewInt(int64(gorpc.PendingBlockNumber))},
		} {
			ginkgo.It("reports the base fees of a fee history ending at "+last.name, func(ctx ginkgo.SpecContext) {
				const blocks = 4
				history := testsuite.MustSucceed(node.Execution.FeeHistory(ctx, blocks, last.number, []float64{50}))
				// The history has the base fee of every block and of the next one.
				gomega.Expect(history.BaseFee).To(gomega.HaveLen(blocks + 1))
				for _, fee := range history.BaseFee {
					gomega.Expect(fee).NotTo(gomega.BeNil())
					gomega.Expect(fee.Sign()).To(gomega.Equal(1))
				}
			})
		}
	},
)

// callObject calls a method that returns a JSON object and returns its fields.
func callObject(ctx context.Context, node *live.Node, method string, args ...any) map[string]any {
	ginkgo.GinkgoHelper()
	var result map[string]any
	gomega.Expect(node.Execution.Client().CallContext(ctx, &result, method, args...)).To(gomega.Succeed())
	gomega.Expect(result).NotTo(gomega.BeNil(), "%s returned null", method)
	return result
}

// expectHeaderFields fails unless a raw header or block carries every header
// field. The pending block has no hash or miner yet.
func expectHeaderFields(raw map[string]any, pending bool) {
	ginkgo.GinkgoHelper()
	if pending {
		gomega.Expect(raw).To(gomega.HaveKeyWithValue("hash", gomega.BeNil()))
		gomega.Expect(raw).To(gomega.HaveKeyWithValue("miner", gomega.BeNil()))
		for _, field := range headerFields {
			if field != "miner" {
				expectFields(raw, field)
			}
		}
		return
	}
	expectFields(raw, "hash")
	expectFields(raw, headerFields...)
}

// expectFields fails unless every named field is present and not null.
func expectFields(fields map[string]any, names ...string) {
	ginkgo.GinkgoHelper()
	for _, name := range names {
		gomega.Expect(fields).To(gomega.HaveKeyWithValue(name, gomega.Not(gomega.BeNil())), "field %q", name)
	}
}
