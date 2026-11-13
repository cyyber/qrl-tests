//go:build e2e

package rpc

import (
	"context"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	qrl "github.com/theQRL/go-qrl"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/qrlclient/gqrlclient"
)

// expectSubscriptions subscribes over WebSocket to new heads and to pending
// transactions, by hash and in full, then sends a transfer: its hash and the
// transfer itself arrive, then the header of the block that includes it.
func expectSubscriptions(ctx context.Context, node *live.Node, chain chaininfo.Info, to common.Address) {
	ginkgo.GinkgoHelper()
	timeout := pendingBlockSlots * chain.SlotDuration

	heads := make(chan *types.Header, 16)
	headSubscription := testsuite.MustSucceed(node.ExecutionWebSocket.SubscribeNewHead(ctx, heads))
	ginkgo.DeferCleanup(headSubscription.Unsubscribe)

	client := gqrlclient.New(node.ExecutionWebSocket.Client())
	hashes := make(chan common.Hash, 16)
	hashSubscription := testsuite.MustSucceed(client.SubscribePendingTransactions(ctx, hashes))
	ginkgo.DeferCleanup(hashSubscription.Unsubscribe)
	transactions := make(chan *types.Transaction, 16)
	transactionSubscription := testsuite.MustSucceed(client.SubscribeFullPendingTransactions(ctx, transactions))
	ginkgo.DeferCleanup(transactionSubscription.Unsubscribe)

	sent := testsuite.MustSucceed(sendTransfer(ctx, node, to))
	receive(ctx, "the pending transaction's hash", hashes, hashSubscription, timeout, func(hash common.Hash) bool {
		return hash == sent.Hash()
	})
	full := receive(ctx, "the pending transaction", transactions, transactionSubscription, timeout, func(tx *types.Transaction) bool {
		return tx.Hash() == sent.Hash()
	})
	gomega.Expect(types.Sender(types.NewZondSigner(node.ChainID), full)).To(gomega.Equal(node.Address))

	receipt := testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, sent))
	// The header hashes to the block's hash only if the subscription sent
	// every field.
	header := receive(ctx, "the header of the transaction's block", heads, headSubscription, timeout, func(header *types.Header) bool {
		return header.Hash() == receipt.BlockHash
	})
	gomega.Expect(header.Number).To(gomega.Equal(receipt.BlockNumber))
	gomega.Expect(header.BaseFee).NotTo(gomega.BeNil())
}

// receive reads a subscription's values until match accepts one.
func receive[T any](ctx context.Context, what string, values <-chan T, subscription qrl.Subscription, timeout time.Duration, match func(T) bool) T {
	ginkgo.GinkgoHelper()
	deadline := time.After(timeout)
	for {
		select {
		case value := <-values:
			if match(value) {
				return value
			}
		case err := <-subscription.Err():
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "subscription to %s", what)
			ginkgo.Fail("the subscription to " + what + " closed")
		case <-deadline:
			ginkgo.Fail("timed out waiting for " + what)
		case <-ctx.Done():
			gomega.Expect(ctx.Err()).NotTo(gomega.HaveOccurred(), "waiting for %s", what)
		}
	}
}
