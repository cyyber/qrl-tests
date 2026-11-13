//go:build e2e

package graphql

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/graphql"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
	qrlwallet "github.com/theQRL/go-qrl/crypto/pqcrypto/wallet"
	"github.com/theQRL/go-qrl/params"
)

// Fees of a transaction that stays in the pool, in planck. The base fee never
// drops below 7, so no block includes it.
var (
	pendingFeeCap = big.NewInt(4)
	pendingTip    = big.NewInt(2)
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "GraphQL E2E suite")
}

var _ = ginkgo.Describe(
	"GraphQL blocks and transactions against a live qrl-package network",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "graphql"),
	func() {
		var (
			node    *live.Node
			mined   *types.Transaction
			receipt *types.Receipt
			pending *types.Transaction
			// sender is the account of the pending transaction.
			sender common.Address

			recipient = common.BytesToAddress([]byte("graphql e2e recipient"))
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			node = testsuite.MustSucceed(testsuite.LoadRuntime().PrimaryNode(ctx))
			gomega.Expect(node.Participant.Execution.GraphQLURL).NotTo(gomega.BeEmpty())
			chain := testsuite.MustSucceed(chaininfo.Load(ctx, node.Beacon))
			signer := types.NewZondSigner(node.ChainID)
			head := testsuite.MustSucceed(node.Execution.HeaderByNumber(ctx, nil))
			tip := testsuite.MustSucceed(node.Execution.SuggestGasTipCap(ctx))
			// Room for the base fee to double.
			feeCap := new(big.Int).Mul(head.BaseFee, big.NewInt(2))
			feeCap.Add(feeCap, tip)

			ginkgo.By("funding a new account, which mines a transaction")
			// The pending transaction blocks its sender's later nonces, so the
			// shared wallet does not send it.
			senderWallet := testsuite.MustSucceed(qrlwallet.Generate(qrlwallet.ML_DSA_87))
			sender = common.Address(senderWallet.GetAddress())
			nonce := testsuite.MustSucceed(node.Execution.PendingNonceAt(ctx, node.Address))
			mined = testsuite.MustSucceed(types.SignNewTx(node.Wallet, signer, &types.DynamicFeeTx{
				ChainID:   node.ChainID,
				Nonce:     nonce,
				GasTipCap: tip,
				GasFeeCap: feeCap,
				Gas:       params.TxGas,
				To:        &sender,
				Value:     big.NewInt(params.Quanta),
			}))
			gomega.Expect(node.Execution.SendTransaction(ctx, mined)).To(gomega.Succeed())
			receipt = testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, mined))

			ginkgo.By("sending a transaction through GraphQL that stays in the pool")
			pending = testsuite.MustSucceed(types.SignNewTx(senderWallet, signer, &types.DynamicFeeTx{
				ChainID:   node.ChainID,
				Nonce:     0,
				GasTipCap: pendingTip,
				GasFeeCap: pendingFeeCap,
				Gas:       params.TxGas,
				To:        &recipient,
				Value:     big.NewInt(1),
			}))
			raw := testsuite.MustSucceed(pending.MarshalBinary())
			sent := query(ctx, node, fmt.Sprintf(`mutation { sendRawTransaction(data: "%s") }`, hexutil.Encode(raw)))
			gomega.Expect(sent).To(gomega.HaveKeyWithValue("sendRawTransaction", pending.Hash().Hex()))

			ginkgo.By("waiting for a finalized block")
			testsuite.WaitForFinalizedBlock(ctx, node.Execution, chain)
		}, ginkgo.NodeTimeout(10*time.Minute))

		// GraphQL takes a block by number or hash, or the latest block with no
		// argument. A range that starts at a negative block number resolves
		// that tag instead: -1 pending, -2 latest, -3 finalized, -4 safe.
		selectors := []struct {
			name    string
			query   func() string
			pending bool
		}{
			{"the latest block", func() string { return "block" }, false},
			{"the genesis block", func() string { return "block(number: 0)" }, false},
			{"a block by number", func() string { return fmt.Sprintf("block(number: %d)", receipt.BlockNumber) }, false},
			{"a block by hash", func() string { return fmt.Sprintf(`block(hash: "%s")`, receipt.BlockHash.Hex()) }, false},
			{"the latest block by tag", func() string { return "blocks(from: -2, to: -2)" }, false},
			{"the finalized block", func() string { return "blocks(from: -3, to: -3)" }, false},
			{"the safe block", func() string { return "blocks(from: -4, to: -4)" }, false},
			{"the pending block", func() string { return "blocks(from: -1, to: -1)" }, true},
		}
		for _, selector := range selectors {
			ginkgo.It("resolves every non-null field of "+selector.name, func(ctx ginkgo.SpecContext) {
				latest := testsuite.MustSucceed(node.Execution.BlockNumber(ctx))
				data := query(ctx, node, fmt.Sprintf(`{ %s { number hash parent { hash } miner { address } stateRoot transactionsRoot receiptsRoot logsBloom extraData gasLimit gasUsed timestamp random rawHeader raw baseFeePerGas nextBaseFeePerGas withdrawalsRoot withdrawals { index validator address amount } transactions { hash type accessList { address storageKeys } maxFeePerGas maxPriorityFeePerGas } } }`, selector.query()))
				var block map[string]any
				for _, value := range data {
					if blocks, ok := value.([]any); ok {
						gomega.Expect(blocks).To(gomega.HaveLen(1))
						value = blocks[0]
					}
					block = value.(map[string]any)
				}
				gomega.Expect(block).NotTo(gomega.BeNil())
				for _, field := range []string{"number", "hash", "miner", "stateRoot", "transactionsRoot", "receiptsRoot", "logsBloom", "extraData", "gasLimit", "gasUsed", "timestamp", "random", "rawHeader", "raw", "baseFeePerGas", "nextBaseFeePerGas", "withdrawalsRoot"} {
					gomega.Expect(block).To(gomega.HaveKeyWithValue(field, gomega.Not(gomega.BeNil())), "field %q", field)
				}
				gomega.Expect(block["withdrawals"]).To(gomega.BeAssignableToTypeOf([]any{}))
				for _, tx := range block["transactions"].([]any) {
					fields := tx.(map[string]any)
					gomega.Expect(fields).To(gomega.HaveKeyWithValue("type", "0x2"))
					gomega.Expect(fields["accessList"]).To(gomega.BeAssignableToTypeOf([]any{}))
					gomega.Expect(fields).To(gomega.HaveKeyWithValue("maxFeePerGas", gomega.Not(gomega.BeNil())))
					gomega.Expect(fields).To(gomega.HaveKeyWithValue("maxPriorityFeePerGas", gomega.Not(gomega.BeNil())))
				}

				if selector.pending {
					number := testsuite.MustSucceed(hexutil.DecodeUint64(block["number"].(string)))
					gomega.Expect(number).To(gomega.BeNumerically(">", latest))
				}
			})
		}

		ginkgo.It("resolves the type, fees and access list of a mined transaction", func(ctx ginkgo.SpecContext) {
			data := query(ctx, node, fmt.Sprintf(`{ transaction(hash: "%s") { type accessList { address } maxFeePerGas maxPriorityFeePerGas status gasUsed effectiveGasPrice block { hash } } }`, mined.Hash().Hex()))
			tx := data["transaction"].(map[string]any)
			gomega.Expect(tx).To(gomega.HaveKeyWithValue("type", "0x2"))
			gomega.Expect(tx).To(gomega.HaveKeyWithValue("accessList", []any{}))
			gomega.Expect(tx).To(gomega.HaveKeyWithValue("maxFeePerGas", hexutil.EncodeBig(mined.GasFeeCap())))
			gomega.Expect(tx).To(gomega.HaveKeyWithValue("maxPriorityFeePerGas", hexutil.EncodeBig(mined.GasTipCap())))
			gomega.Expect(tx).To(gomega.HaveKeyWithValue("status", "0x1"))
			gomega.Expect(tx).To(gomega.HaveKeyWithValue("gasUsed", hexutil.EncodeUint64(receipt.GasUsed)))
			gomega.Expect(tx).To(gomega.HaveKeyWithValue("effectiveGasPrice", hexutil.EncodeBig(receipt.EffectiveGasPrice)))
			gomega.Expect(tx).To(gomega.HaveKeyWithValue("block", map[string]any{"hash": receipt.BlockHash.Hex()}))
		})

		ginkgo.It("resolves a pending transaction, whose inclusion fields stay null", func(ctx ginkgo.SpecContext) {
			data := query(ctx, node, `{ pending { transactions { hash type accessList { address } maxFeePerGas maxPriorityFeePerGas status gasUsed effectiveGasPrice block { hash } } } }`)
			var tx map[string]any
			for _, candidate := range data["pending"].(map[string]any)["transactions"].([]any) {
				if fields := candidate.(map[string]any); fields["hash"] == pending.Hash().Hex() {
					tx = fields
				}
			}
			gomega.Expect(tx).NotTo(gomega.BeNil(), "the pending transaction is not in the pool")
			gomega.Expect(tx).To(gomega.HaveKeyWithValue("type", "0x2"))
			gomega.Expect(tx).To(gomega.HaveKeyWithValue("accessList", []any{}))
			gomega.Expect(tx).To(gomega.HaveKeyWithValue("maxFeePerGas", hexutil.EncodeBig(pendingFeeCap)))
			gomega.Expect(tx).To(gomega.HaveKeyWithValue("maxPriorityFeePerGas", hexutil.EncodeBig(pendingTip)))
			for _, field := range []string{"status", "gasUsed", "effectiveGasPrice", "block"} {
				gomega.Expect(tx).To(gomega.HaveKeyWithValue(field, gomega.BeNil()), "field %q", field)
			}
		})

		ginkgo.It("resolves an account's balance, transaction count, code and storage at a block and in the pending state", func(ctx ginkgo.SpecContext) {
			// The sender has what the mined transaction sent it. No block
			// includes the transaction it sent, which only the pending
			// transaction count has.
			funded := hexutil.EncodeBig(big.NewInt(params.Quanta))
			data := query(ctx, node, fmt.Sprintf(`{
				block(number: %d) { account(address: "%s") { address balance transactionCount code storage(slot: "%s") } }
				pending { transactionCount account(address: "%s") { address balance transactionCount } }
			}`, receipt.BlockNumber, sender.Hex(), common.Hash{}.Hex(), sender.Hex()))
			gomega.Expect(data["block"]).To(gomega.Equal(map[string]any{"account": map[string]any{
				"address":          sender.Hex(),
				"balance":          funded,
				"transactionCount": "0x0",
				"code":             "0x",
				"storage":          hexutil.Encode(make([]byte, 2*common.HashLength)),
			}}))
			pendingState := data["pending"].(map[string]any)
			gomega.Expect(pendingState["account"]).To(gomega.Equal(map[string]any{
				"address":          sender.Hex(),
				"balance":          funded,
				"transactionCount": "0x1",
			}))
			// The pending state counts the pool's transactions.
			count := testsuite.MustSucceed(hexutil.DecodeUint64(pendingState["transactionCount"].(string)))
			gomega.Expect(count).To(gomega.BeNumerically(">=", 1))
		})

		ginkgo.It("runs a call and estimates its gas at a block and in the pending state", func(ctx ginkgo.SpecContext) {
			transfer := fmt.Sprintf(`{from: "%s", to: "%s", value: "0x1"}`, node.Address.Hex(), recipient.Hex())
			selection := fmt.Sprintf(`call(data: %[1]s) { data gasUsed status } estimateGas(data: %[1]s)`, transfer)
			data := query(ctx, node, fmt.Sprintf(`{ block(number: %d) { %s } pending { %s } }`, receipt.BlockNumber, selection, selection))
			gas := hexutil.EncodeUint64(params.TxGas)
			for _, state := range []string{"block", "pending"} {
				gomega.Expect(data[state]).To(gomega.Equal(map[string]any{
					"call":        map[string]any{"data": "0x", "gasUsed": gas, "status": "0x1"},
					"estimateGas": gas,
				}), "state %q", state)
			}
		})

		ginkgo.It("resolves the gas price, the priority fee and the chain ID, and no synchronisation in progress", func(ctx ginkgo.SpecContext) {
			data := query(ctx, node, `{ gasPrice maxPriorityFeePerGas chainID syncing { startingBlock currentBlock highestBlock } }`)
			tip := testsuite.MustSucceed(hexutil.DecodeBig(data["maxPriorityFeePerGas"].(string)))
			gomega.Expect(tip.Sign()).To(gomega.Equal(1))
			// The gas price is the priority fee above the base fee.
			price := testsuite.MustSucceed(hexutil.DecodeBig(data["gasPrice"].(string)))
			gomega.Expect(price.Cmp(tip)).To(gomega.Equal(1))
			gomega.Expect(data).To(gomega.HaveKeyWithValue("chainID", hexutil.EncodeBig(node.ChainID)))
			gomega.Expect(data).To(gomega.HaveKeyWithValue("syncing", gomega.BeNil()))
		})
	},
)

// query runs a GraphQL query against the node and returns its data, failing
// on any GraphQL error.
func query(ctx context.Context, node *live.Node, query string) map[string]any {
	ginkgo.GinkgoHelper()
	return testsuite.MustSucceed(graphql.Query(ctx, node.Participant.Execution.GraphQLURL, query))
}
