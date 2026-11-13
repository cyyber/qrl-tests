//go:build e2e

package parity

import (
	"context"
	"math/big"

	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/suites/execution/abi/contracts"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	qrl "github.com/theQRL/go-qrl"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
)

// expectAccountState reads an account's balance, transaction count, code and
// first storage slot at a block through JSON-RPC, qrlclient and GraphQL.
func expectAccountState(ctx context.Context, node *live.Node, account common.Address, block rpcBlock) {
	ginkgo.GinkgoHelper()
	number := block.number()
	numberArg := testsuite.MustSucceed(hexutil.DecodeBig(number))
	slot := common.Hash{}
	state := object{
		"address":          account.Hex(),
		"balance":          call[string](ctx, node, "qrl_getBalance", account, number),
		"transactionCount": call[string](ctx, node, "qrl_getTransactionCount", account, number),
		"code":             call[string](ctx, node, "qrl_getCode", account, number),
		"storage":          call[string](ctx, node, "qrl_getStorageAt", account, slot.Hex(), number),
	}

	gomega.Expect(object{
		"address":          account.Hex(),
		"balance":          hexutil.EncodeBig(testsuite.MustSucceed(node.Execution.BalanceAt(ctx, account, numberArg))),
		"transactionCount": hexutil.EncodeUint64(testsuite.MustSucceed(node.Execution.NonceAt(ctx, account, numberArg))),
		"code":             hexutil.Encode(testsuite.MustSucceed(node.Execution.CodeAt(ctx, account, numberArg))),
		"storage":          hexutil.Encode(testsuite.MustSucceed(node.Execution.StorageAt(ctx, account, slot, numberArg))),
	}).To(gomega.Equal(state), "qrlclient state of %s at block %s", account, number)

	graphQL := query(ctx, node, `{ block(hash: "%s") { account(address: "%s") { address balance transactionCount code storage(slot: "%s") } } }`,
		block.hash(), account.Hex(), slot.Hex())["block"].(object)["account"]
	gomega.Expect(graphQL).To(gomega.Equal(state), "GraphQL state of %s at block %s", account, number)
}

// expectCall calls EventEmitter's plusOne, which runs in the library it
// links, at a block through JSON-RPC, qrlclient and GraphQL, and estimates
// the call's gas through JSON-RPC and GraphQL.
func expectCall(ctx context.Context, node *live.Node, emitter common.Address, block rpcBlock) {
	ginkgo.GinkgoHelper()
	number := block.number()
	parsed := testsuite.MustSucceed(contracts.EventEmitterMetaData.GetAbi())
	data := testsuite.MustSucceed(parsed.Pack("plusOne", big.NewInt(41)))
	args := object{"from": node.Address, "to": emitter, "data": hexutil.Bytes(data)}

	result := call[string](ctx, node, "qrl_call", args, number)
	gomega.Expect(testsuite.MustSucceed(parsed.Unpack("plusOne", testsuite.MustSucceed(hexutil.Decode(result))))).To(gomega.Equal([]any{big.NewInt(42)}))
	typed := testsuite.MustSucceed(node.Execution.CallContract(ctx,
		qrl.CallMsg{From: node.Address, To: &emitter, Data: data}, testsuite.MustSucceed(hexutil.DecodeBig(number))))
	gomega.Expect(hexutil.Encode(typed)).To(gomega.Equal(result), "qrlclient CallContract at block %s", number)

	graphQL := query(ctx, node, `{ block(hash: "%[1]s") { call(data: %[2]s) { data status } estimateGas(data: %[2]s) } }`, block.hash(),
		`{from: "`+node.Address.Hex()+`", to: "`+emitter.Hex()+`", data: "`+hexutil.Encode(data)+`"}`)["block"].(object)
	gomega.Expect(graphQL["call"]).To(gomega.Equal(object{"data": result, "status": "0x1"}), "GraphQL call at block %s", number)
	gomega.Expect(graphQL["estimateGas"]).To(gomega.Equal(call[string](ctx, node, "qrl_estimateGas", args, number)), "GraphQL gas estimate at block %s", number)
}
