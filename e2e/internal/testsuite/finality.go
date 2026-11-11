package testsuite

import (
	"context"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// A healthy chain finalizes within this many epochs of its head.
const maxFinalityLagEpochs = 3

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
