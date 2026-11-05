//go:build e2e

package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"testing"
	"time"

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
	gorpc "github.com/theQRL/go-qrl/rpc"
)

// Fees of a transaction that stays in the pool, in planck. The base fee never
// drops below 7, so no block includes it.
var (
	pendingFeeCap = big.NewInt(4)
	pendingTip    = big.NewInt(2)
)

// finalityTimeout bounds the wait for the first finalized checkpoint, a few
// 40-second epochs after genesis.
const finalityTimeout = 5 * time.Minute

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

			recipient = common.BytesToAddress([]byte("graphql e2e recipient"))
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			node = testsuite.MustSucceed(testsuite.LoadRuntime().PrimaryNode(ctx))
			gomega.Expect(node.Participant.Execution.GraphQLURL).NotTo(gomega.BeEmpty())
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

			ginkgo.By("sending a transaction through GraphQL that stays in the pool")
			pending = testsuite.MustSucceed(types.SignNewTx(sender, signer, &types.DynamicFeeTx{
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
			gomega.Eventually(func() error {
				_, err := node.Execution.HeaderByNumber(ctx, big.NewInt(int64(gorpc.FinalizedBlockNumber)))
				return err
			}).WithContext(ctx).WithTimeout(finalityTimeout).WithPolling(time.Second).Should(gomega.Succeed())
		}, ginkgo.NodeTimeout(finalityTimeout+2*time.Minute))

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

				number := testsuite.MustSucceed(hexutil.DecodeUint64(block["number"].(string)))
				if selector.pending {
					gomega.Expect(number).To(gomega.BeNumerically(">", latest))
					return
				}
				// The same block over JSON-RPC carries the same values.
				var header map[string]any
				gomega.Expect(node.Execution.Client().CallContext(ctx, &header, "qrl_getHeaderByHash", block["hash"])).To(gomega.Succeed())
				gomega.Expect(header).NotTo(gomega.BeNil())
				for graphqlField, rpcField := range map[string]string{
					"number":          "number",
					"baseFeePerGas":   "baseFeePerGas",
					"withdrawalsRoot": "withdrawalsRoot",
					"random":          "prevRandao",
					"stateRoot":       "stateRoot",
				} {
					gomega.Expect(block[graphqlField]).To(gomega.Equal(header[rpcField]), "field %q", graphqlField)
				}
				gomega.Expect(block["miner"]).To(gomega.HaveKeyWithValue("address", header["miner"]))
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
	},
)

// query runs a GraphQL query against the node and returns its data, failing
// on any GraphQL error.
func query(ctx context.Context, node *live.Node, query string) map[string]any {
	ginkgo.GinkgoHelper()
	body := testsuite.MustSucceed(json.Marshal(map[string]string{"query": query}))
	request := testsuite.MustSucceed(http.NewRequestWithContext(ctx, http.MethodPost, node.Participant.Execution.GraphQLURL, bytes.NewReader(body)))
	request.Header.Set("Content-Type", "application/json")
	response := testsuite.MustSucceed(http.DefaultClient.Do(request))
	defer response.Body.Close()
	gomega.Expect(response.StatusCode).To(gomega.Equal(http.StatusOK))

	var result struct {
		Data   map[string]any    `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	gomega.Expect(json.NewDecoder(response.Body).Decode(&result)).To(gomega.Succeed())
	gomega.Expect(result.Errors).To(gomega.BeEmpty(), "query %s", query)
	return result.Data
}
