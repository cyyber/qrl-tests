//go:build e2e

package slashing

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const (
	primaryParticipant   = 0
	duplicateParticipant = 1
	watcherParticipant   = 2

	maxEpochDuration = 40 * time.Second

	detectionEpochs = 4
)

// suiteDuplicated is every validator whose key a scenario has run in a second
// client. Ginkgo may run the scenarios in either order on the same network,
// and a failed attempt's duplicate can still be slashed later, so any of these
// may end up slashed, and no other validator may.
var suiteDuplicated []uint64

// duplicating records that the scenario is about to run the validator's key in
// a second client.
func duplicating(index uint64) {
	if !slices.Contains(suiteDuplicated, index) {
		suiteDuplicated = append(suiteDuplicated, index)
	}
}

// loadNodes returns the devnet's nodes: the primary's, the duplicate's, and
// the watcher's, which runs no validators.
func loadNodes(ctx context.Context, runtime *live.Runtime) []*live.Node {
	ginkgo.GinkgoHelper()
	nodes := testsuite.MustSucceed(runtime.Nodes(ctx))
	gomega.Expect(nodes).To(gomega.HaveLen(watcherParticipant+1),
		"the suite runs validators on two nodes, and a third watches")
	return nodes
}

// expectShortEpochs fails fast on epochs longer than the suite's timeouts
// assume.
func expectShortEpochs(chain chaininfo.Info) {
	ginkgo.GinkgoHelper()
	gomega.Expect(chain.EpochDuration).To(gomega.BeNumerically("<=", maxEpochDuration),
		"the suite's timeouts assume epochs of at most %s", maxEpochDuration)
}

// planNextEpoch waits for an epoch in whose first half pick accepts a duty of
// the next epoch, and returns that duty. Planning only in the first half
// leaves at least half an epoch to start the duplicate validator client, even
// for a duty in the next epoch's first slot.
func planNextEpoch[Duty any](
	ctx context.Context, client *beacon.Client, chain chaininfo.Info, pick func(epoch uint64) (Duty, bool),
) Duty {
	ginkgo.GinkgoHelper()
	for {
		now := time.Now()
		head := testsuite.MustSucceed(client.HeadSlot(ctx))
		epoch := max(chain.EpochAt(now), chain.Epoch(head))
		start := slotStart(chain, epoch*chain.SlotsPerEpoch)
		if now.Before(start.Add(chain.EpochDuration / 2)) {
			if duty, ok := pick(epoch); ok {
				return duty
			}
		}
		gomega.Expect(testsuite.SleepUntil(ctx, start.Add(chain.EpochDuration))).To(gomega.Succeed())
	}
}

// awaitSlashed reports whether the validator is slashed within
// detectionEpochs.
func awaitSlashed(ctx context.Context, val rewards.Validator) bool {
	ginkgo.GinkgoHelper()
	deadline := time.Now().Add(detectionEpochs * val.Chain.EpochDuration)
	index := strconv.FormatUint(val.Index, 10)
	for time.Now().Before(deadline) {
		record := testsuite.MustSucceed(val.Client.Validator(ctx, index))
		if record.Slashed {
			return true
		}
		gomega.Expect(testsuite.SleepUntil(ctx, time.Now().Add(rewards.PollInterval))).To(gomega.Succeed())
	}
	ginkgo.GinkgoWriter.Printf("validator %d was not slashed within %d epochs\n", val.Index, detectionEpochs)
	return false
}

// findEvidence searches the blocks from the given slot up to the head, and
// returns the first one that slashes the validator, with its evidence.
func findEvidence(ctx context.Context, val rewards.Validator, from uint64) (uint64, slashingEvidence) {
	ginkgo.GinkgoHelper()
	head := testsuite.MustSucceed(val.Client.HeadSlot(ctx))
	for slot := from; slot <= head; slot++ {
		operations, err := val.Client.BlockOperations(ctx, strconv.FormatUint(slot, 10))
		if beacon.IsNotFound(err) {
			continue
		}
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		if evidence, found := evidenceAgainst(operations, val.Index); found {
			return slot, evidence
		}
	}
	ginkgo.Fail("no block from slot " + strconv.FormatUint(from, 10) + " slashes the validator")
	return 0, slashingEvidence{}
}

func slotStart(chain chaininfo.Info, slot uint64) time.Time {
	return chain.GenesisTime.Add(time.Duration(slot) * chain.SlotDuration)
}
