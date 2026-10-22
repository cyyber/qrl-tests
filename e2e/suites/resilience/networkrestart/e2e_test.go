//go:build e2e

package networkrestart

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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
	stopOutageEpochs = 1
	// killOutageEpochs stays at one epoch until cyyber/qrysm#109 is fixed:
	// the second participant's beacon node initial-syncs from the anchor as
	// its only peer. One epoch passes; at five the sync ends at a stale slot
	// and the network forks, and at twenty it never ends.
	killOutageEpochs      = 1
	resumeTimeoutEpochs   = 4
	finalityTimeoutEpochs = 8
)

// killPhase is the point within a slot at which every node is killed. A
// validator proposes at the start of its slot, attests a third of the way in
// and aggregates two thirds in, so what each node has on disk and in flight
// depends on where the kill lands.
type killPhase struct {
	name string
	// thirds is the offset into the slot, in thirds of a slot.
	thirds int64
}

var killPhases = []killPhase{
	{name: "at the start of a slot, while the block is proposed", thirds: 0},
}

func (phase killPhase) offset(chain chaininfo.Info) time.Duration {
	return time.Duration(phase.thirds) * chain.SlotDuration / 3
}

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Network restart E2E suite")
}

// The outages run in one ordered Describe because a restarted service is
// published on new host ports that only the runtime that restarted it knows.
var _ = ginkgo.Describe(
	"A network whose nodes all go down at once",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "resilience", "network-restart"),
	func() {
		var (
			runtime *live.Runtime
			nodes   []*live.Node
			chain   chaininfo.Info
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

		expectDown := func(ctx context.Context) {
			for index, node := range nodes {
				_, err := node.Beacon.HeadSlot(ctx)
				gomega.Expect(err).To(gomega.HaveOccurred(), "the beacon node of participant %d still answers", index+1)

				_, err = node.Execution.BlockNumber(ctx)
				gomega.Expect(err).To(gomega.HaveOccurred(), "the execution client of participant %d still answers", index+1)
			}
		}

		startParticipants := func(ctx context.Context) {
			gomega.Expect(runtime.StartParticipants(ctx)).To(gomega.Succeed())
			nodes = testsuite.MustSucceed(runtime.Nodes(ctx))
		}

		// expectProposing waits until every participant has a head proposed
		// since the restart. No block is proposed during the outage, so a
		// head beyond it is one.
		expectProposing := func(ctx context.Context, lastSlot, outageEpochs uint64) {
			for index, node := range nodes {
				gomega.Eventually(node.Beacon.HeadSlot).WithContext(ctx).
					WithTimeout(resumeTimeoutEpochs*chain.EpochDuration).
					WithPolling(chain.SlotDuration).
					Should(gomega.BeNumerically(">", lastSlot+outageEpochs*chain.SlotsPerEpoch),
						"participant %d", index+1)
			}
		}

		// expectBlock checks that every participant still has the block at its
		// slot.
		expectBlock := func(ctx context.Context, block beacon.BlockHeader, name string) {
			for index, node := range nodes {
				kept, err := node.Beacon.BlockHeader(ctx, strconv.FormatUint(block.Slot, 10))
				gomega.Expect(err).NotTo(gomega.HaveOccurred(),
					"participant %d has no block at the %s slot %d", index+1, name, block.Slot)
				gomega.Expect(kept.Root).To(gomega.Equal(block.Root),
					"participant %d changed the block at the %s slot", index+1, name)
			}
		}

		// expectFinalizing waits until every participant finalizes an epoch
		// after lastSlot, which only votes cast since the restart can do,
		// and checks that they all finalized the same chain.
		expectFinalizing := func(ctx context.Context, lastSlot uint64) {
			for index, node := range nodes {
				gomega.Eventually(finalizedEpoch(node)).WithContext(ctx).
					WithTimeout(finalityTimeoutEpochs*chain.EpochDuration).
					WithPolling(chain.SlotDuration).
					Should(gomega.BeNumerically(">", chain.Epoch(lastSlot)), "participant %d", index+1)
			}

			finalized := testsuite.MustSucceed(nodes[0].Beacon.BlockHeader(ctx, "finalized"))
			for index, node := range nodes[1:] {
				same := testsuite.MustSucceed(node.Beacon.BlockHeader(ctx, strconv.FormatUint(finalized.Slot, 10)))
				gomega.Expect(same.Root).To(gomega.Equal(finalized.Root),
					"participant %d is on another chain than participant 1", index+2)
			}
		}

		expectNoSlashing := func(ctx context.Context) {
			gomega.Expect(testsuite.MustSucceed(nodes[0].Beacon.SlashedValidators(ctx))).To(gomega.BeEmpty())
		}

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime = testsuite.LoadRuntime()
			nodes = testsuite.MustSucceed(runtime.Nodes(ctx))
			gomega.Expect(len(nodes)).To(gomega.BeNumerically(">", 1), "the scenario needs several participants")
			chain = testsuite.MustSucceed(chaininfo.Load(ctx, nodes[0].Beacon))
		}, ginkgo.NodeTimeout(time.Minute))

		ginkgo.It("finalizes before the outage", func(ctx ginkgo.SpecContext) {
			gomega.Eventually(finalizedEpoch(nodes[0])).WithContext(ctx).
				WithTimeout(finalityTimeoutEpochs * chain.EpochDuration).
				WithPolling(chain.SlotDuration).
				Should(gomega.BeNumerically(">", 0))
		}, ginkgo.SpecTimeout(7*time.Minute))

		ginkgo.Context("when every node stops", func() {
			// head is the head every participant agreed on once the
			// validators stopped.
			var head beacon.BlockHeader

			ginkgo.It("agrees on a head once its validators stop", func(ctx ginkgo.SpecContext) {
				gomega.Expect(runtime.StopValidators(ctx)).To(gomega.Succeed())
				// Without validators, only a block already in flight can still
				// change the head. Once a slot has passed and every participant
				// agrees on it, no later block can replace it.
				pause(ctx, chain.SlotDuration)
				gomega.Eventually(func(g gomega.Gomega, ctx context.Context) {
					anchorHead, err := nodes[0].Beacon.BlockHeader(ctx, "head")
					g.Expect(err).NotTo(gomega.HaveOccurred())
					for index, node := range nodes[1:] {
						otherHead, err := node.Beacon.BlockHeader(ctx, "head")
						g.Expect(err).NotTo(gomega.HaveOccurred())
						g.Expect(otherHead).To(gomega.Equal(anchorHead), "participant %d has another head", index+2)
					}
					head = anchorHead
				}).WithContext(ctx).
					WithTimeout(chain.EpochDuration).
					WithPolling(chain.SlotDuration).
					Should(gomega.Succeed())
			}, ginkgo.SpecTimeout(2*time.Minute))

			ginkgo.It("stops every node", func(ctx ginkgo.SpecContext) {
				gomega.Expect(runtime.StopNodes(ctx)).To(gomega.Succeed())
				expectDown(ctx)
				pause(ctx, stopOutageEpochs*chain.EpochDuration)
			}, ginkgo.SpecTimeout(2*time.Minute))

			ginkgo.It("starts every node again", func(ctx ginkgo.SpecContext) {
				startParticipants(ctx)
			}, ginkgo.SpecTimeout(2*time.Minute))

			ginkgo.It("resumes from its old head", func(ctx ginkgo.SpecContext) {
				expectProposing(ctx, head.Slot, stopOutageEpochs)
				// The chain continues from the head it had, not from an
				// earlier checkpoint.
				expectBlock(ctx, head, "old head")
			}, ginkgo.SpecTimeout(4*time.Minute))

			ginkgo.It("finalizes again on one chain", func(ctx ginkgo.SpecContext) {
				expectFinalizing(ctx, head.Slot)
			}, ginkgo.SpecTimeout(6*time.Minute))

			ginkgo.It("slashes no validator", func(ctx ginkgo.SpecContext) {
				expectNoSlashing(ctx)
			}, ginkgo.SpecTimeout(time.Minute))
		})

		// A kill at a known phase of a slot, since what a node had written
		// and what it was doing when the power went depend on it.
		for _, phase := range killPhases {
			ginkgo.Context("when every node is killed "+phase.name, func() {
				var (
					// killed is the point of the chain's clock at which the
					// first kill was sent. No block can be proposed after its
					// slot before the restart.
					killed chaininfo.SlotPoint
					// finalized is the block participant 1 had finalized before
					// the kill. No participant may lose it.
					finalized beacon.BlockHeader
				)

				ginkgo.It("kills every node", func(ctx ginkgo.SpecContext) {
					finalized = testsuite.MustSucceed(nodes[0].Beacon.BlockHeader(ctx, "finalized"))
					head := testsuite.MustSucceed(nodes[0].Beacon.BlockHeader(ctx, "head"))
					// The containers are found before the wait, so the kills
					// follow the chosen moment as closely as Docker allows.
					exits, err := runtime.KillParticipants(ctx, func(ctx context.Context) error {
						var err error
						killed, err = chain.WaitForSlotOffset(ctx, phase.offset(chain))
						return err
					})
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					// Tie a failure of the specs that follow to the phase. The
					// exit times are the Docker host's, which may run a little
					// apart from this clock.
					var exited []string
					for _, exit := range exits {
						exited = append(exited, exit.Name+" at "+chain.SlotPointAt(exit.ExitedAt).String())
					}
					record := fmt.Sprintf(
						"killed every node %s: SIGKILL sent at %s (target offset %s); exited: %s; head was slot %d, finalized slot %d",
						phase.name, killed, phase.offset(chain), strings.Join(exited, ", "), head.Slot, finalized.Slot,
					)
					ginkgo.GinkgoWriter.Println(record)
					ginkgo.AddReportEntry("kill", record)
					for _, exit := range exits {
						gomega.Expect(chain.SlotPointAt(exit.ExitedAt).Slot).To(gomega.Equal(killed.Slot),
							"%s did not exit in the slot the kill was timed for", exit.Name)
					}

					expectDown(ctx)
					pause(ctx, killOutageEpochs*chain.EpochDuration)
				}, ginkgo.SpecTimeout(2*time.Minute))

				ginkgo.It("starts every node again", func(ctx ginkgo.SpecContext) {
					startParticipants(ctx)
				}, ginkgo.SpecTimeout(2*time.Minute))

				ginkgo.It("proposes again and keeps its finalized blocks", func(ctx ginkgo.SpecContext) {
					expectProposing(ctx, killed.Slot, killOutageEpochs)
					expectBlock(ctx, finalized, "finalized")
				}, ginkgo.SpecTimeout(4*time.Minute))

				ginkgo.It("finalizes again on one chain", func(ctx ginkgo.SpecContext) {
					expectFinalizing(ctx, killed.Slot)
				}, ginkgo.SpecTimeout(6*time.Minute))

				ginkgo.It("slashes no validator", func(ctx ginkgo.SpecContext) {
					expectNoSlashing(ctx)
				}, ginkgo.SpecTimeout(time.Minute))
			})
		}
	},
)
