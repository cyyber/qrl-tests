package testsuite

import (
	"context"
	"math/big"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/qrlclient"
	"github.com/theQRL/go-qrl/rpc"
)

const (
	// A healthy chain finalizes within this many epochs of its head.
	maxFinalityLagEpochs = 3
	firstFinalityEpochs  = 8
)

// WaitForFinalizedBlock waits until the execution client has a finalized
// block. A new chain finalizes its first block a few epochs after genesis.
func WaitForFinalizedBlock(ctx context.Context, client *qrlclient.Client, chain chaininfo.Info) {
	ginkgo.GinkgoHelper()
	finalized := big.NewInt(int64(rpc.FinalizedBlockNumber))
	gomega.Eventually(func() error {
		_, err := client.HeaderByNumber(ctx, finalized)
		return err
	}).WithContext(ctx).
		WithTimeout(firstFinalityEpochs*chain.EpochDuration).
		WithPolling(chain.SlotDuration).
		Should(gomega.Succeed(), "the chain has no finalized block")
}

// ExpectFinalizing checks that the chain has finalized an epoch within
// maxFinalityLagEpochs of its head.
func ExpectFinalizing(ctx context.Context, client *beacon.Client, chain chaininfo.Info) {
	ginkgo.GinkgoHelper()
	head := MustSucceed(client.HeadSlot(ctx))
	checkpoints := MustSucceed(client.FinalityCheckpoints(ctx))
	gomega.Expect(checkpoints.Finalized.Epoch+maxFinalityLagEpochs).To(gomega.BeNumerically(">=", chain.Epoch(head)),
		"finalized epoch %d at head slot %d", checkpoints.Finalized.Epoch, head)
}

// AwaitFinalizedEpoch waits up to withinEpochs epochs for the chain to
// finalize epoch or a later one.
func AwaitFinalizedEpoch(ctx context.Context, client *beacon.Client, chain chaininfo.Info, epoch, withinEpochs uint64) {
	ginkgo.GinkgoHelper()
	gomega.Eventually(func(ctx context.Context) (uint64, error) {
		checkpoints, err := client.FinalityCheckpoints(ctx)
		return checkpoints.Finalized.Epoch, err
	}).WithContext(ctx).
		WithTimeout(time.Duration(withinEpochs)*chain.EpochDuration).
		WithPolling(chain.SlotDuration).
		Should(gomega.BeNumerically(">=", epoch), "finalized epoch, waiting for epoch %d", epoch)
}
