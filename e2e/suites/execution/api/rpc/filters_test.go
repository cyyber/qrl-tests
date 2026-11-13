//go:build e2e

package rpc

import (
	"context"
	"math/big"

	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/suites/execution/abi/contracts"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
)

// expectFilters installs a block, a pending transaction and a log filter,
// calls EventEmitter to log, and reads the filters' changes until they report
// the call's block, hash and logs. A filter over the call's block returns the
// same logs, and an uninstalled filter is not found.
func expectFilters(ctx context.Context, node *live.Node, chain chaininfo.Info, emitter common.Address, binding *contracts.EventEmitter) {
	ginkgo.GinkgoHelper()
	blockFilter := call[string](ctx, node, "qrl_newBlockFilter")
	pendingFilter := call[string](ctx, node, "qrl_newPendingTransactionFilter")
	logFilter := call[string](ctx, node, "qrl_newFilter", map[string]any{"address": emitter})

	ginkgo.By("calling EventEmitter to log")
	auth := testsuite.MustSucceed(bind.NewKeyedTransactorWithChainID(node.Wallet, node.ChainID))
	auth.Context = ctx
	tx := testsuite.MustSucceed(binding.Store(auth, big.NewInt(6), big.NewInt(-7), [64]byte{8}, node.Address, []byte("stored"), "filtered", true))
	receipt := testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, tx))
	want := callObject(ctx, node, "qrl_getTransactionReceipt", tx.Hash())["logs"].([]any)
	gomega.Expect(want).NotTo(gomega.BeEmpty())

	ginkgo.By("reading the filters' changes")
	// A filter reports each change once, so the changes add up over the
	// reads.
	var blocks, hashes, logs []any
	gomega.Eventually(func(g gomega.Gomega) {
		blocks = append(blocks, call[[]any](ctx, node, "qrl_getFilterChanges", blockFilter)...)
		hashes = append(hashes, call[[]any](ctx, node, "qrl_getFilterChanges", pendingFilter)...)
		logs = append(logs, call[[]any](ctx, node, "qrl_getFilterChanges", logFilter)...)
		g.Expect(blocks).To(gomega.ContainElement(receipt.BlockHash.Hex()))
		g.Expect(hashes).To(gomega.ContainElement(tx.Hash().Hex()))
		g.Expect(logs).To(gomega.Equal(want))
	}).WithContext(ctx).
		WithTimeout(pendingBlockSlots * chain.SlotDuration).
		WithPolling(pendingBlockPolling).
		Should(gomega.Succeed())
	gomega.Expect(call[[]any](ctx, node, "qrl_getFilterChanges", logFilter)).To(gomega.BeEmpty())

	ginkgo.By("reading the logs of a filter over the call's block")
	criteria := map[string]any{
		"fromBlock": hexutil.EncodeBig(receipt.BlockNumber),
		"toBlock":   hexutil.EncodeBig(receipt.BlockNumber),
		"address":   emitter,
	}
	rangeFilter := call[string](ctx, node, "qrl_newFilter", criteria)
	gomega.Expect(call[[]any](ctx, node, "qrl_getFilterLogs", rangeFilter)).To(gomega.Equal(want))
	gomega.Expect(call[[]any](ctx, node, "qrl_getLogs", criteria)).To(gomega.Equal(want))

	ginkgo.By("uninstalling the filters")
	for _, filter := range []string{blockFilter, pendingFilter, logFilter, rangeFilter} {
		gomega.Expect(call[bool](ctx, node, "qrl_uninstallFilter", filter)).To(gomega.BeTrue())
		gomega.Expect(call[bool](ctx, node, "qrl_uninstallFilter", filter)).To(gomega.BeFalse())
		var changes []any
		err := node.Execution.Client().CallContext(ctx, &changes, "qrl_getFilterChanges", filter)
		gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("filter not found")))
	}
}
