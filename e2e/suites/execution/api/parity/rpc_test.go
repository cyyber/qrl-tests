//go:build e2e

package parity

import (
	"context"
	"maps"
	"math/big"

	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
	gorpc "github.com/theQRL/go-qrl/rpc"
)

// object is a JSON object as an API returns it.
type object = map[string]any

// headerFields are the fields of qrl_getHeaderBy*, which qrl_getBlockBy*
// returns too.
var headerFields = []string{
	"hash", "parentHash", "miner", "stateRoot", "transactionsRoot", "receiptsRoot", "logsBloom",
	"number", "gasLimit", "gasUsed", "timestamp", "extraData", "prevRandao", "baseFeePerGas",
	"withdrawalsRoot",
}

// rpcBlock is a block as qrl_getBlockByNumber returns it with full
// transactions, and its receipts as qrl_getBlockReceipts returns them.
type rpcBlock struct {
	full     object
	receipts []object
}

func (block rpcBlock) number() string {
	return block.full["number"].(string)
}

func (block rpcBlock) hash() string {
	return block.full["hash"].(string)
}

func (block rpcBlock) header() object {
	header := object{}
	for _, field := range headerFields {
		header[field] = block.full[field]
	}
	return header
}

func (block rpcBlock) transactions() []object {
	return toObjects(block.full["transactions"].([]any))
}

// withTransactionHashes is the block as qrl_getBlockBy* returns it without
// full transactions.
func (block rpcBlock) withTransactionHashes() object {
	result := maps.Clone(block.full)
	hashes := make([]any, 0)
	for _, transaction := range block.transactions() {
		hashes = append(hashes, transaction["hash"])
	}
	result["transactions"] = hashes
	return result
}

// logs are the logs of the block's receipts, in order.
func (block rpcBlock) logs() []object {
	logs := make([]object, 0)
	for _, receipt := range block.receipts {
		logs = append(logs, toObjects(receipt["logs"].([]any))...)
	}
	return logs
}

// call calls a JSON-RPC method and decodes its result.
func call[T any](ctx context.Context, node *live.Node, method string, args ...any) T {
	ginkgo.GinkgoHelper()
	var result T
	gomega.Expect(node.Execution.Client().CallContext(ctx, &result, method, args...)).To(gomega.Succeed(), "%s%v", method, args)
	return result
}

// callWebSocket calls a JSON-RPC method over WebSocket and decodes its
// result.
func callWebSocket[T any](ctx context.Context, node *live.Node, method string, args ...any) T {
	ginkgo.GinkgoHelper()
	var result T
	gomega.Expect(node.ExecutionWebSocket.Client().CallContext(ctx, &result, method, args...)).To(gomega.Succeed(), "%s%v over WebSocket", method, args)
	return result
}

// readBlocks reads every block from genesis to last, and its receipts, by
// number.
func readBlocks(ctx context.Context, node *live.Node, last uint64) []rpcBlock {
	ginkgo.GinkgoHelper()
	blocks := make([]rpcBlock, 0, last+1)
	for number := range last + 1 {
		arg := hexutil.EncodeUint64(number)
		block := rpcBlock{
			full:     call[object](ctx, node, "qrl_getBlockByNumber", arg, true),
			receipts: call[[]object](ctx, node, "qrl_getBlockReceipts", arg),
		}
		gomega.Expect(block.full).NotTo(gomega.BeNil(), "block %s", arg)
		blocks = append(blocks, block)
	}
	return blocks
}

// tagReads are the block a tag names, read through every path that takes a
// tag.
type tagReads struct {
	header, hashes, full object
	receipts             []object
	typedHeader          *types.Header
	typedBlock           *types.Block
	graphQL              map[string]any
}

// readTag reads the block tag names through every path. The tag can move on
// while they run, so it reads them again until the tag names the same block
// before and after them.
func readTag(ctx context.Context, node *live.Node, tag gorpc.BlockNumber) tagReads {
	ginkgo.GinkgoHelper()
	number := big.NewInt(int64(tag))
	graphQL := map[string]string{
		"blocks(from: tag, to: tag)": `{ result: blocks(from: %[1]d, to: %[1]d) { %[2]s } }`,
	}
	if tag == gorpc.LatestBlockNumber {
		graphQL["block"] = `{ result: block { %[2]s } }`
	}
	for {
		before := call[object](ctx, node, "qrl_getHeaderByNumber", tag.String())
		reads := tagReads{
			header:      before,
			hashes:      call[object](ctx, node, "qrl_getBlockByNumber", tag.String(), false),
			full:        call[object](ctx, node, "qrl_getBlockByNumber", tag.String(), true),
			receipts:    call[[]object](ctx, node, "qrl_getBlockReceipts", tag.String()),
			typedHeader: testsuite.MustSucceed(node.Execution.HeaderByNumber(ctx, number)),
			typedBlock:  testsuite.MustSucceed(node.Execution.BlockByNumber(ctx, number)),
			graphQL:     map[string]any{},
		}
		for name, format := range graphQL {
			result := query(ctx, node, format, int64(tag), graphQLBlockFields)["result"]
			if blocks, ok := result.([]any); ok {
				gomega.Expect(blocks).To(gomega.HaveLen(1), "GraphQL %s for %s", name, tag)
				result = blocks[0]
			}
			reads.graphQL[name] = result
		}
		after := call[object](ctx, node, "qrl_getHeaderByNumber", tag.String())
		if after["hash"] == before["hash"] {
			return reads
		}
	}
}

// headerValues encodes a decoded header as the API does. Its hash is
// computed from the decoded fields.
func headerValues(header *types.Header) object {
	return object{
		"hash":             header.Hash().Hex(),
		"parentHash":       header.ParentHash.Hex(),
		"miner":            header.Coinbase.Hex(),
		"stateRoot":        header.Root.Hex(),
		"transactionsRoot": header.TxHash.Hex(),
		"receiptsRoot":     header.ReceiptHash.Hex(),
		"logsBloom":        hexutil.Encode(header.Bloom.Bytes()),
		"number":           hexutil.EncodeBig(header.Number),
		"gasLimit":         hexutil.EncodeUint64(header.GasLimit),
		"gasUsed":          hexutil.EncodeUint64(header.GasUsed),
		"timestamp":        hexutil.EncodeUint64(header.Time),
		"extraData":        hexutil.Encode(header.Extra),
		"prevRandao":       header.Random.Hex(),
		"baseFeePerGas":    hexutil.EncodeBig(header.BaseFee),
		"withdrawalsRoot":  header.WithdrawalsHash.Hex(),
	}
}

// blockValues encodes a decoded block as the API does without full
// transactions. Its size is that of its encoding.
func blockValues(block *types.Block) object {
	values := headerValues(block.Header())
	values["size"] = hexutil.EncodeUint64(block.Size())
	transactions := make([]any, 0)
	for _, transaction := range block.Transactions() {
		transactions = append(transactions, transaction.Hash().Hex())
	}
	values["transactions"] = transactions
	withdrawals := make([]any, 0)
	for _, withdrawal := range block.Withdrawals() {
		withdrawals = append(withdrawals, object{
			"index":          hexutil.EncodeUint64(withdrawal.Index),
			"validatorIndex": hexutil.EncodeUint64(withdrawal.Validator),
			"address":        withdrawal.Address.Hex(),
			"amount":         hexutil.EncodeUint64(withdrawal.Amount),
		})
	}
	values["withdrawals"] = withdrawals
	return values
}

// accessListValues encodes an access list as the API does.
func accessListValues(accessList types.AccessList) []any {
	values := make([]any, 0)
	for _, tuple := range accessList {
		keys := make([]any, 0)
		for _, key := range tuple.StorageKeys {
			keys = append(keys, key.Hex())
		}
		values = append(values, object{"address": tuple.Address.Hex(), "storageKeys": keys})
	}
	return values
}

// receiptFields are the receipt fields GraphQL has too.
func receiptFields(receipt object) object {
	return object{
		"status":            receipt["status"],
		"gasUsed":           receipt["gasUsed"],
		"cumulativeGasUsed": receipt["cumulativeGasUsed"],
		"effectiveGasPrice": receipt["effectiveGasPrice"],
		"contractAddress":   receipt["contractAddress"],
		"logs":              withoutRemoved(toObjects(receipt["logs"].([]any))),
	}
}

// withoutRemoved drops the removed flag, which GraphQL logs do not have.
func withoutRemoved(logs []object) []object {
	result := make([]object, 0, len(logs))
	for _, log := range logs {
		copied := maps.Clone(log)
		delete(copied, "removed")
		result = append(result, copied)
	}
	return result
}

// equal matches want, or nil when want is nil, which gomega.Equal refuses.
func equal(want any) gomega.OmegaMatcher {
	if want == nil {
		return gomega.BeNil()
	}
	return gomega.Equal(want)
}

func toObjects(values []any) []object {
	objects := make([]object, 0, len(values))
	for _, value := range values {
		objects = append(objects, value.(object))
	}
	return objects
}
