//go:build e2e

package parity

import (
	"context"
	"fmt"
	"math/big"

	"github.com/cyyber/qrl-tests/e2e/internal/graphql"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/rlp"
)

const (
	graphQLBlockFields = `hash number parent { hash } miner { address } stateRoot transactionsRoot
		receiptsRoot logsBloom extraData gasLimit gasUsed timestamp prevRandao: random
		baseFeePerGas nextBaseFeePerGas withdrawalsRoot rawHeader raw transactionCount
		transactions { hash } withdrawals { index validatorIndex: validator address amount }`
	graphQLTransactionFields = `hash nonce transactionIndex: index from { address } to { address }
		value gas gasPrice: effectiveGasPrice maxFeePerGas maxPriorityFeePerGas input: inputData
		type accessList { address storageKeys } descriptor extraParams publicKey signature
		block { hash number } raw effectiveTip`
	graphQLLogFields = `logIndex: index account { address } topics data
		transaction { hash index block { hash number } }`
	graphQLReceiptFields = `status gasUsed cumulativeGasUsed effectiveGasPrice
		createdContract { address } logs { ` + graphQLLogFields + ` }`
)

// query runs a GraphQL query, formatted from format and args, and returns its
// data.
func query(ctx context.Context, node *live.Node, format string, args ...any) object {
	ginkgo.GinkgoHelper()
	return testsuite.MustSucceed(graphql.Query(ctx, node.Participant.Execution.GraphQLURL, fmt.Sprintf(format, args...)))
}

// expectGraphQLBlock checks a GraphQL block against the same block over
// JSON-RPC. The selections alias GraphQL fields to their JSON-RPC names. The
// next base fee is the next block's base fee, when there is one.
func expectGraphQLBlock(graphQLBlock object, block rpcBlock, next *rpcBlock) {
	ginkgo.GinkgoHelper()
	hash := block.hash()
	rawHeader := testsuite.MustSucceed(hexutil.Decode(graphQLBlock["rawHeader"].(string)))
	raw := testsuite.MustSucceed(hexutil.Decode(graphQLBlock["raw"].(string)))

	values := object{}
	for _, field := range headerFields {
		if value, ok := graphQLBlock[field]; ok {
			values[field] = value
		}
	}
	values["miner"] = address(graphQLBlock["miner"])
	// Genesis has no parent, and a zero parent hash over JSON-RPC.
	values["parentHash"] = common.Hash{}.Hex()
	if parent, ok := graphQLBlock["parent"].(object); ok {
		values["parentHash"] = parent["hash"]
	}
	// A block's size is the length of its encoding, which raw is.
	values["size"] = hexutil.EncodeUint64(uint64(len(raw)))
	transactions := make([]any, 0)
	for _, transaction := range toObjects(graphQLBlock["transactions"].([]any)) {
		transactions = append(transactions, transaction["hash"])
	}
	values["transactions"] = transactions
	values["withdrawals"] = graphQLBlock["withdrawals"]
	gomega.Expect(values).To(gomega.Equal(block.withTransactionHashes()), "GraphQL block %s", hash)
	gomega.Expect(graphQLBlock["transactionCount"]).To(gomega.Equal(hexutil.EncodeUint64(uint64(len(block.transactions())))), "GraphQL transaction count of block %s", hash)

	var header types.Header
	gomega.Expect(rlp.DecodeBytes(rawHeader, &header)).To(gomega.Succeed(), "decode GraphQL raw header of block %s", hash)
	gomega.Expect(headerValues(&header)).To(gomega.Equal(block.header()), "GraphQL raw header of block %s", hash)
	var decoded types.Block
	gomega.Expect(rlp.DecodeBytes(raw, &decoded)).To(gomega.Succeed(), "decode GraphQL raw block %s", hash)
	gomega.Expect(blockValues(&decoded)).To(gomega.Equal(block.withTransactionHashes()), "GraphQL raw block %s", hash)

	if next != nil {
		gomega.Expect(graphQLBlock["nextBaseFeePerGas"]).To(gomega.Equal(next.full["baseFeePerGas"]), "GraphQL next base fee of block %s", hash)
	}
}

// expectGraphQLTransaction checks a GraphQL transaction against the same
// transaction over JSON-RPC: its fields, its encoding raw, and its tip, which
// is what its gas price pays above the block's base fee. GraphQL has no chain
// ID per transaction, so the chain's stands in.
func expectGraphQLTransaction(graphQLTransaction, transaction object, chainID any, raw, baseFee string) {
	ginkgo.GinkgoHelper()
	hash := transaction["hash"]
	block := graphQLTransaction["block"].(object)
	values := object{
		"chainId":     chainID,
		"from":        address(graphQLTransaction["from"]),
		"to":          address(graphQLTransaction["to"]),
		"blockHash":   block["hash"],
		"blockNumber": block["number"],
	}
	for field, value := range graphQLTransaction {
		switch field {
		case "from", "to", "block", "raw", "effectiveTip":
		default:
			values[field] = value
		}
	}
	gomega.Expect(values).To(gomega.Equal(transaction), "GraphQL transaction %s", hash)
	gomega.Expect(graphQLTransaction["raw"]).To(gomega.Equal(raw), "GraphQL raw transaction %s", hash)

	gasPrice := testsuite.MustSucceed(hexutil.DecodeBig(transaction["gasPrice"].(string)))
	tip := new(big.Int).Sub(gasPrice, testsuite.MustSucceed(hexutil.DecodeBig(baseFee)))
	gomega.Expect(graphQLTransaction["effectiveTip"]).To(gomega.Equal(hexutil.EncodeBig(tip)), "GraphQL effective tip of transaction %s", hash)
}

// fromGraphQLReceipt names a GraphQL transaction's receipt fields as
// JSON-RPC does.
func fromGraphQLReceipt(receipt object) object {
	return object{
		"status":            receipt["status"],
		"gasUsed":           receipt["gasUsed"],
		"cumulativeGasUsed": receipt["cumulativeGasUsed"],
		"effectiveGasPrice": receipt["effectiveGasPrice"],
		"contractAddress":   address(receipt["createdContract"]),
		"logs":              fromGraphQLLogs(receipt["logs"]),
	}
}

// fromGraphQLLogs names GraphQL logs' fields as JSON-RPC does. A null list
// stays nil, which differs from every list JSON-RPC returns.
func fromGraphQLLogs(logs any) []object {
	if logs == nil {
		return nil
	}
	result := make([]object, 0)
	for _, log := range toObjects(logs.([]any)) {
		transaction := log["transaction"].(object)
		block := transaction["block"].(object)
		result = append(result, object{
			"logIndex":         log["logIndex"],
			"address":          address(log["account"]),
			"topics":           log["topics"],
			"data":             log["data"],
			"transactionHash":  transaction["hash"],
			"transactionIndex": transaction["index"],
			"blockHash":        block["hash"],
			"blockNumber":      block["number"],
		})
	}
	return result
}

// address is a GraphQL account's address, or nil for no account.
func address(account any) any {
	if account == nil {
		return nil
	}
	return account.(object)["address"]
}
