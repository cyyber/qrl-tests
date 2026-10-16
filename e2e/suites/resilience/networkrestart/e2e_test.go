//go:build e2e

package networkrestart

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const (
	// outageEpochs is how long every node stays down.
	outageEpochs = 1
	// restartEpochs bounds the wait for a head past the outage. The anchor
	// proposes within slots; the other beacon node first has to sync from it.
	restartEpochs = 4
	// finalityEpochs bounds the wait for a finalized checkpoint, from genesis
	// and again after the restart. Finality needs three epochs at best.
	finalityEpochs = 8
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Network restart E2E suite")
}

var _ = ginkgo.Describe(
	"A network whose nodes all stop at once",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "resilience", "network-restart"),
	func() {
		var (
			runtime *live.Runtime
			nodes   []*live.Node
			chain   chaininfo.Info
			// head is the head every participant agreed on once the
			// validators stopped.
			head beacon.BlockHeader
		)

		finalizedEpoch := func(node *live.Node) func(context.Context) (uint64, error) {
			return func(ctx context.Context) (uint64, error) {
				checkpoints, err := node.Beacon.FinalityCheckpoints(ctx)
				return checkpoints.Finalized.Epoch, err
			}
		}

		pause := func(ctx context.Context, duration time.Duration) {
			select {
			case <-time.After(duration):
			case <-ctx.Done():
			}
		}

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime = testsuite.LoadRuntime()
			gomega.Expect(runtime.CheckRestartable()).To(gomega.Succeed())
			nodes = testsuite.MustSucceed(runtime.Nodes(ctx))
			gomega.Expect(len(nodes)).To(gomega.BeNumerically(">", 1), "the scenario needs several participants")
			chain = testsuite.MustSucceed(chaininfo.Load(ctx, nodes[0].Beacon))
		}, ginkgo.NodeTimeout(time.Minute))

		// A failed spec skips the rest; whatever it left stopped is started
		// here, so the diagnostics of a failed run see a running network.
		ginkgo.AfterAll(func(ctx ginkgo.SpecContext) {
			if runtime != nil && runtime.HasStoppedParticipants() {
				gomega.Expect(runtime.StartParticipants(ctx)).To(gomega.Succeed())
			}
		}, ginkgo.NodeTimeout(3*time.Minute))

		ginkgo.It("finalizes before the outage", func(ctx ginkgo.SpecContext) {
			gomega.Eventually(finalizedEpoch(nodes[0])).WithContext(ctx).
				WithTimeout(finalityEpochs * chain.EpochDuration).
				WithPolling(chain.SlotDuration).
				Should(gomega.BeNumerically(">", 0))
		}, ginkgo.SpecTimeout(7*time.Minute))

		ginkgo.It("stops every node", func(ctx ginkgo.SpecContext) {
			gomega.Expect(runtime.StopValidators(ctx)).To(gomega.Succeed())
			// Without validators, only a block already in flight can still
			// change the head. Once a slot has passed and every participant
			// agrees on it, no later block can replace it.
			pause(ctx, chain.SlotDuration)
			gomega.Eventually(func(g gomega.Gomega, ctx context.Context) {
				anchor, err := nodes[0].Beacon.BlockHeader(ctx, "head")
				g.Expect(err).NotTo(gomega.HaveOccurred())
				for index, node := range nodes[1:] {
					other, err := node.Beacon.BlockHeader(ctx, "head")
					g.Expect(err).NotTo(gomega.HaveOccurred())
					g.Expect(other).To(gomega.Equal(anchor), "participant %d has another head", index+2)
				}
				head = anchor
			}).WithContext(ctx).
				WithTimeout(chain.EpochDuration).
				WithPolling(chain.SlotDuration).
				Should(gomega.Succeed())

			gomega.Expect(runtime.StopNodes(ctx)).To(gomega.Succeed())
			for index, node := range nodes {
				_, err := node.Beacon.HeadSlot(ctx)
				gomega.Expect(err).To(gomega.HaveOccurred(), "the beacon node of participant %d still answers", index+1)
			}

			pause(ctx, outageEpochs*chain.EpochDuration)
		}, ginkgo.SpecTimeout(4*time.Minute))

		ginkgo.It("proposes again after the restart", func(ctx ginkgo.SpecContext) {
			gomega.Expect(runtime.StartParticipants(ctx)).To(gomega.Succeed())
			nodes = testsuite.MustSucceed(runtime.Nodes(ctx))

			for index, node := range nodes {
				// No block was proposed during the outage, so a head beyond
				// it is a block proposed since the restart.
				gomega.Eventually(node.Beacon.HeadSlot).WithContext(ctx).
					WithTimeout(restartEpochs*chain.EpochDuration).
					WithPolling(chain.SlotDuration).
					Should(gomega.BeNumerically(">", head.Slot+outageEpochs*chain.SlotsPerEpoch),
						"participant %d", index+1)

				// The chain continues from the head it had, not from an
				// earlier checkpoint.
				kept := testsuite.MustSucceed(node.Beacon.BlockHeader(ctx, strconv.FormatUint(head.Slot, 10)))
				gomega.Expect(kept.Root).To(gomega.Equal(head.Root),
					"participant %d changed the block at the old head slot", index+1)
			}
		}, ginkgo.SpecTimeout(6*time.Minute))

		ginkgo.It("finalizes again on one chain", func(ctx ginkgo.SpecContext) {
			for index, node := range nodes {
				// An epoch after the outage began can only be finalized by
				// votes cast since the restart.
				gomega.Eventually(finalizedEpoch(node)).WithContext(ctx).
					WithTimeout(finalityEpochs*chain.EpochDuration).
					WithPolling(chain.SlotDuration).
					Should(gomega.BeNumerically(">", chain.Epoch(head.Slot)), "participant %d", index+1)
			}

			finalized := testsuite.MustSucceed(nodes[0].Beacon.BlockHeader(ctx, "finalized"))
			for index, node := range nodes[1:] {
				same := testsuite.MustSucceed(node.Beacon.BlockHeader(ctx, strconv.FormatUint(finalized.Slot, 10)))
				gomega.Expect(same.Root).To(gomega.Equal(finalized.Root),
					"participant %d is on another chain than participant 1", index+2)
			}
			gomega.Expect(testsuite.MustSucceed(nodes[0].Beacon.SlashedValidators(ctx))).To(gomega.BeEmpty(),
				"a validator was slashed")
		}, ginkgo.SpecTimeout(7*time.Minute))
	},
)
