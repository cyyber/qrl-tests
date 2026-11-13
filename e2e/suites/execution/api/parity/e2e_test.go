//go:build e2e

package parity

import (
	"fmt"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
	gorpc "github.com/theQRL/go-qrl/rpc"
)

const specTimeout = 5 * time.Minute

func TestE2E(t *testing.T) {
	testsuite.Run(t, "API parity E2E suite")
}

var _ = ginkgo.Describe(
	"Blocks, transactions, receipts, logs and state through every JSON-RPC and GraphQL path against a live qrl-package network",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.ContinueOnFailure,
	ginkgo.Label("e2e", "parity"),
	func() {
		var (
			node    *live.Node
			emitted history
			// blocks are every block from genesis to the head once the suite's
			// transactions and a withdrawal are in.
			blocks []rpcBlock
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			node = testsuite.MustSucceed(testsuite.LoadRuntime().PrimaryWithWebSocket(ctx))
			gomega.Expect(node.Participant.Execution.GraphQLURL).NotTo(gomega.BeEmpty())
			chain := testsuite.MustSucceed(chaininfo.Load(ctx, node.Beacon))

			emitted = sendTransactions(ctx, node)

			ginkgo.By("waiting for a finalized block and a block with withdrawals")
			testsuite.WaitForFinalizedBlock(ctx, node.Execution, chain)
			waitForWithdrawals(ctx, node, chain)

			ginkgo.By("reading every block from genesis and its receipts by number")
			blocks = readBlocks(ctx, node, testsuite.MustSucceed(node.Execution.BlockNumber(ctx)))
			var transactions, logs, withWithdrawals int
			for _, block := range blocks {
				transactions += len(block.transactions())
				logs += len(block.logs())
				if len(block.full["withdrawals"].([]any)) > 0 {
					withWithdrawals++
				}
			}
			ginkgo.AddReportEntry("blocks from genesis", fmt.Sprintf("%d blocks, %d with withdrawals, %d transactions and %d logs",
				len(blocks), withWithdrawals, transactions, logs))
		}, ginkgo.NodeTimeout(10*time.Minute))

		ginkgo.It("returns every block alike by number and by hash, as a header or a block with or without full transactions, over HTTP and WebSocket, and as qrlclient decodes it", func(ctx ginkgo.SpecContext) {
			for _, block := range blocks {
				number, hash := block.number(), block.hash()
				header, hashes := block.header(), block.withTransactionHashes()
				gomega.Expect(call[object](ctx, node, "qrl_getHeaderByNumber", number)).To(gomega.Equal(header), "qrl_getHeaderByNumber(%s)", number)
				gomega.Expect(call[object](ctx, node, "qrl_getHeaderByHash", hash)).To(gomega.Equal(header), "qrl_getHeaderByHash(%s)", hash)
				gomega.Expect(call[object](ctx, node, "qrl_getBlockByNumber", number, false)).To(gomega.Equal(hashes), "qrl_getBlockByNumber(%s, false)", number)
				gomega.Expect(call[object](ctx, node, "qrl_getBlockByHash", hash, false)).To(gomega.Equal(hashes), "qrl_getBlockByHash(%s, false)", hash)
				gomega.Expect(call[object](ctx, node, "qrl_getBlockByHash", hash, true)).To(gomega.Equal(block.full), "qrl_getBlockByHash(%s, true)", hash)
				gomega.Expect(callWebSocket[object](ctx, node, "qrl_getBlockByNumber", number, true)).To(gomega.Equal(block.full), "qrl_getBlockByNumber(%s, true) over WebSocket", number)
				gomega.Expect(callWebSocket[[]object](ctx, node, "qrl_getBlockReceipts", number)).To(gomega.Equal(block.receipts), "qrl_getBlockReceipts(%s) over WebSocket", number)
				count := hexutil.EncodeUint64(uint64(len(block.transactions())))
				gomega.Expect(call[string](ctx, node, "qrl_getBlockTransactionCountByNumber", number)).To(gomega.Equal(count), "qrl_getBlockTransactionCountByNumber(%s)", number)
				gomega.Expect(call[string](ctx, node, "qrl_getBlockTransactionCountByHash", hash)).To(gomega.Equal(count), "qrl_getBlockTransactionCountByHash(%s)", hash)

				// The decoded header hashes to the block's hash only if
				// qrlclient kept every field.
				numberArg := testsuite.MustSucceed(hexutil.DecodeBig(number))
				hashArg := common.HexToHash(hash)
				gomega.Expect(headerValues(testsuite.MustSucceed(node.Execution.HeaderByNumber(ctx, numberArg)))).To(gomega.Equal(header), "qrlclient HeaderByNumber(%s)", number)
				gomega.Expect(headerValues(testsuite.MustSucceed(node.Execution.HeaderByHash(ctx, hashArg)))).To(gomega.Equal(header), "qrlclient HeaderByHash(%s)", hash)
				gomega.Expect(blockValues(testsuite.MustSucceed(node.Execution.BlockByNumber(ctx, numberArg)))).To(gomega.Equal(hashes), "qrlclient BlockByNumber(%s)", number)
				gomega.Expect(blockValues(testsuite.MustSucceed(node.Execution.BlockByHash(ctx, hashArg)))).To(gomega.Equal(hashes), "qrlclient BlockByHash(%s)", hash)
			}
		}, ginkgo.SpecTimeout(specTimeout))

		for _, tag := range []gorpc.BlockNumber{gorpc.LatestBlockNumber, gorpc.SafeBlockNumber, gorpc.FinalizedBlockNumber} {
			ginkgo.It("returns the "+tag.String()+" block alike by tag and by hash, through JSON-RPC, qrlclient and GraphQL", func(ctx ginkgo.SpecContext) {
				reads := readTag(ctx, node, tag)
				hash := reads.header["hash"].(string)
				byHash := rpcBlock{
					full:     call[object](ctx, node, "qrl_getBlockByHash", hash, true),
					receipts: call[[]object](ctx, node, "qrl_getBlockReceipts", hash),
				}
				gomega.Expect(reads.header).To(gomega.Equal(byHash.header()), "qrl_getHeaderByNumber(%s)", tag)
				gomega.Expect(reads.hashes).To(gomega.Equal(byHash.withTransactionHashes()), "qrl_getBlockByNumber(%s, false)", tag)
				gomega.Expect(reads.full).To(gomega.Equal(byHash.full), "qrl_getBlockByNumber(%s, true)", tag)
				gomega.Expect(reads.receipts).To(gomega.Equal(byHash.receipts), "qrl_getBlockReceipts(%s)", tag)
				gomega.Expect(headerValues(reads.typedHeader)).To(gomega.Equal(byHash.header()), "qrlclient HeaderByNumber(%s)", tag)
				gomega.Expect(blockValues(reads.typedBlock)).To(gomega.Equal(byHash.withTransactionHashes()), "qrlclient BlockByNumber(%s)", tag)

				graphQLByHash := query(ctx, node, `{ block(hash: "%s") { %s } }`, hash, graphQLBlockFields)["block"]
				for name, graphQLBlock := range reads.graphQL {
					gomega.Expect(graphQLBlock).To(gomega.Equal(graphQLByHash), "GraphQL %s", name)
				}
				expectGraphQLBlock(graphQLByHash.(object), byHash, nil)
			}, ginkgo.SpecTimeout(specTimeout))
		}

		ginkgo.It("resolves every block through GraphQL by range, number and hash with the values JSON-RPC returns", func(ctx ginkgo.SpecContext) {
			last := len(blocks) - 1
			ranged := query(ctx, node, `{ blocks(from: 0, to: %d) { %s } }`, last, graphQLBlockFields)["blocks"].([]any)
			gomega.Expect(ranged).To(gomega.HaveLen(len(blocks)), "GraphQL blocks(from: 0, to: %d)", last)
			for index, block := range blocks {
				hash := block.hash()
				gomega.Expect(query(ctx, node, `{ block(number: %d) { %s } }`, index, graphQLBlockFields)["block"]).To(gomega.Equal(ranged[index]), "GraphQL block(number: %d)", index)
				gomega.Expect(query(ctx, node, `{ block(hash: "%s") { %s } }`, hash, graphQLBlockFields)["block"]).To(gomega.Equal(ranged[index]), "GraphQL block(hash: %s)", hash)
				var next *rpcBlock
				if index < last {
					next = &blocks[index+1]
				}
				expectGraphQLBlock(ranged[index].(object), block, next)

				// The debug API returns the encodings GraphQL does.
				graphQLBlock := ranged[index].(object)
				for _, id := range []string{block.number(), hash} {
					gomega.Expect(call[string](ctx, node, "debug_getRawHeader", id)).To(gomega.Equal(graphQLBlock["rawHeader"]), "debug_getRawHeader(%s)", id)
					gomega.Expect(call[string](ctx, node, "debug_getRawBlock", id)).To(gomega.Equal(graphQLBlock["raw"]), "debug_getRawBlock(%s)", id)
				}
			}
		}, ginkgo.SpecTimeout(specTimeout))

		ginkgo.It("returns every transaction alike by hash, by block hash or number and index, inside its block, as qrlclient decodes it and through GraphQL", func(ctx ginkgo.SpecContext) {
			chainID := query(ctx, node, `{ chainID }`)["chainID"]
			for _, block := range blocks {
				number, hash := block.number(), block.hash()
				inBlock := query(ctx, node, `{ block(hash: "%s") { transactions { %s } } }`, hash, graphQLTransactionFields)["block"].(object)["transactions"].([]any)
				gomega.Expect(inBlock).To(gomega.HaveLen(len(block.transactions())), "GraphQL transactions of block %s", hash)
				for index, transaction := range block.transactions() {
					txHash := transaction["hash"].(string)
					position := hexutil.EncodeUint64(uint64(index))
					gomega.Expect(call[object](ctx, node, "qrl_getTransactionByHash", txHash)).To(gomega.Equal(transaction), "qrl_getTransactionByHash(%s)", txHash)
					gomega.Expect(call[object](ctx, node, "qrl_getTransactionByBlockHashAndIndex", hash, position)).To(gomega.Equal(transaction), "qrl_getTransactionByBlockHashAndIndex(%s, %s)", hash, position)
					gomega.Expect(call[object](ctx, node, "qrl_getTransactionByBlockNumberAndIndex", number, position)).To(gomega.Equal(transaction), "qrl_getTransactionByBlockNumberAndIndex(%s, %s)", number, position)

					raw := call[string](ctx, node, "qrl_getRawTransactionByHash", txHash)
					gomega.Expect(call[string](ctx, node, "qrl_getRawTransactionByBlockHashAndIndex", hash, position)).To(gomega.Equal(raw), "qrl_getRawTransactionByBlockHashAndIndex(%s, %s)", hash, position)
					gomega.Expect(call[string](ctx, node, "qrl_getRawTransactionByBlockNumberAndIndex", number, position)).To(gomega.Equal(raw), "qrl_getRawTransactionByBlockNumberAndIndex(%s, %s)", number, position)
					gomega.Expect(call[string](ctx, node, "debug_getRawTransaction", txHash)).To(gomega.Equal(raw), "debug_getRawTransaction(%s)", txHash)
					var decoded types.Transaction
					gomega.Expect(decoded.UnmarshalBinary(testsuite.MustSucceed(hexutil.Decode(raw)))).To(gomega.Succeed(), "decode raw transaction %s", txHash)
					gomega.Expect(decoded.Hash().Hex()).To(gomega.Equal(txHash), "raw transaction %s", txHash)

					typed, isPending, err := node.Execution.TransactionByHash(ctx, common.HexToHash(txHash))
					gomega.Expect(err).NotTo(gomega.HaveOccurred(), "qrlclient TransactionByHash(%s)", txHash)
					gomega.Expect(isPending).To(gomega.BeFalse(), "qrlclient TransactionByHash(%s)", txHash)
					gomega.Expect(typed.Hash().Hex()).To(gomega.Equal(txHash), "qrlclient TransactionByHash(%s)", txHash)
					typed = testsuite.MustSucceed(node.Execution.TransactionInBlock(ctx, common.HexToHash(hash), uint(index)))
					gomega.Expect(typed.Hash().Hex()).To(gomega.Equal(txHash), "qrlclient TransactionInBlock(%s, %d)", hash, index)

					byHash := query(ctx, node, `{ transaction(hash: "%s") { %s } }`, txHash, graphQLTransactionFields)["transaction"]
					at := query(ctx, node, `{ block(hash: "%s") { transactionAt(index: %d) { %s } } }`, hash, index, graphQLTransactionFields)["block"].(object)["transactionAt"]
					gomega.Expect(at).To(gomega.Equal(byHash), "GraphQL transactionAt(index: %d) of block %s", index, hash)
					gomega.Expect(inBlock[index]).To(gomega.Equal(byHash), "GraphQL transaction %d of block %s", index, hash)
					expectGraphQLTransaction(byHash.(object), transaction, chainID, raw, block.full["baseFeePerGas"].(string))
				}
			}

			// Every path returned the transaction qrl_getTransactionByHash
			// does, so its access list is the one every path returns.
			listed := call[object](ctx, node, "qrl_getTransactionByHash", emitted.accessList.Hash().Hex())
			gomega.Expect(listed["accessList"]).To(gomega.Equal(accessListValues(emitted.accessList.AccessList())), "access list of transaction %s", emitted.accessList.Hash())
		}, ginkgo.SpecTimeout(specTimeout))

		ginkgo.It("returns every receipt alike by block, by transaction and through GraphQL", func(ctx ginkgo.SpecContext) {
			for _, block := range blocks {
				hash := block.hash()
				gomega.Expect(call[[]object](ctx, node, "qrl_getBlockReceipts", hash)).To(gomega.Equal(block.receipts), "qrl_getBlockReceipts(%s)", hash)
				transactions := block.transactions()
				gomega.Expect(block.receipts).To(gomega.HaveLen(len(transactions)), "receipts of block %s", hash)
				graphQLReceipts := query(ctx, node, `{ block(hash: "%s") { transactions { rawReceipt %s } } }`, hash, graphQLReceiptFields)["block"].(object)["transactions"].([]any)
				gomega.Expect(graphQLReceipts).To(gomega.HaveLen(len(transactions)), "GraphQL transactions of block %s", hash)
				rawReceipts := call[[]string](ctx, node, "debug_getRawReceipts", hash)
				gomega.Expect(rawReceipts).To(gomega.HaveLen(len(transactions)), "debug_getRawReceipts(%s)", hash)
				for index, receipt := range block.receipts {
					transaction := transactions[index]
					txHash := transaction["hash"].(string)
					gomega.Expect(call[object](ctx, node, "qrl_getTransactionReceipt", txHash)).To(gomega.Equal(receipt), "qrl_getTransactionReceipt(%s)", txHash)
					// The receipt names its transaction and price as the
					// transaction does.
					for receiptField, transactionField := range map[string]string{
						"transactionHash":   "hash",
						"transactionIndex":  "transactionIndex",
						"blockHash":         "blockHash",
						"blockNumber":       "blockNumber",
						"from":              "from",
						"to":                "to",
						"type":              "type",
						"effectiveGasPrice": "gasPrice",
					} {
						gomega.Expect(receipt[receiptField]).To(equal(transaction[transactionField]), "receipt %s of transaction %s", receiptField, txHash)
					}
					gomega.Expect(fromGraphQLReceipt(graphQLReceipts[index].(object))).To(gomega.Equal(receiptFields(receipt)), "GraphQL receipt of transaction %s", txHash)
					gomega.Expect(graphQLReceipts[index].(object)["rawReceipt"]).To(gomega.Equal(rawReceipts[index]), "GraphQL raw receipt of transaction %s", txHash)
					var decoded types.Receipt
					gomega.Expect(decoded.UnmarshalBinary(testsuite.MustSucceed(hexutil.Decode(rawReceipts[index])))).To(gomega.Succeed(), "decode raw receipt of transaction %s", txHash)
					gomega.Expect(hexutil.EncodeUint64(decoded.Status)).To(gomega.Equal(receipt["status"]), "raw receipt status of transaction %s", txHash)
					gomega.Expect(hexutil.EncodeUint64(decoded.CumulativeGasUsed)).To(gomega.Equal(receipt["cumulativeGasUsed"]), "raw receipt gas of transaction %s", txHash)
					gomega.Expect(decoded.Logs).To(gomega.HaveLen(len(receipt["logs"].([]any))), "raw receipt logs of transaction %s", txHash)
				}
			}
		}, ginkgo.SpecTimeout(specTimeout))

		ginkgo.It("returns an account's state, a call's result and its gas estimate alike through JSON-RPC, qrlclient and GraphQL", func(ctx ginkgo.SpecContext) {
			// The head moves on, so every path reads the state at one block.
			last := blocks[len(blocks)-1]
			for _, account := range []common.Address{node.Address, emitted.emitter} {
				expectAccountState(ctx, node, account, last)
			}
			expectCall(ctx, node, emitted.emitter, last)
		}, ginkgo.SpecTimeout(specTimeout))

		ginkgo.It("returns every log alike by block range, by block hash, inside receipts and through GraphQL", func(ctx ginkgo.SpecContext) {
			all := make([]object, 0)
			for _, block := range blocks {
				hash := block.hash()
				logs := block.logs()
				gomega.Expect(call[[]object](ctx, node, "qrl_getLogs", object{"blockHash": hash})).To(gomega.Equal(logs), "qrl_getLogs for block %s", hash)
				graphQLLogs := query(ctx, node, `{ block(hash: "%s") { logs(filter: {}) { %s } } }`, hash, graphQLLogFields)["block"].(object)["logs"]
				gomega.Expect(fromGraphQLLogs(graphQLLogs)).To(gomega.Equal(withoutRemoved(logs)), "GraphQL logs of block %s", hash)
				all = append(all, logs...)
			}
			last := blocks[len(blocks)-1].number()
			gomega.Expect(call[[]object](ctx, node, "qrl_getLogs", object{"fromBlock": "0x0", "toBlock": last})).To(gomega.Equal(all), "qrl_getLogs from genesis to %s", last)
			graphQLLogs := query(ctx, node, `{ logs(filter: {fromBlock: 0, toBlock: %d}) { %s } }`, len(blocks)-1, graphQLLogFields)["logs"]
			gomega.Expect(fromGraphQLLogs(graphQLLogs)).To(gomega.Equal(withoutRemoved(all)), "GraphQL logs from genesis to %s", last)

			var fromEmitter int
			for _, log := range all {
				gomega.Expect(log).To(gomega.HaveKeyWithValue("removed", false))
				if log["address"] == emitted.emitter.Hex() {
					fromEmitter++
				}
			}
			gomega.Expect(fromEmitter).To(gomega.Equal(emitted.logs), "logs of EventEmitter %s", emitted.emitter)
		}, ginkgo.SpecTimeout(specTimeout))
	},
)
