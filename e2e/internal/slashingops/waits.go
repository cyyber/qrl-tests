package slashingops

import (
	"context"
	"strconv"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const (
	maxEpochDuration = 40 * time.Second

	detectionEpochs = 4
)

// ExpectShortEpochs fails fast on epochs longer than the suites' timeouts
// assume.
func ExpectShortEpochs(chain chaininfo.Info) {
	ginkgo.GinkgoHelper()
	gomega.Expect(chain.EpochDuration).To(gomega.BeNumerically("<=", maxEpochDuration),
		"the suite's timeouts assume epochs of at most %s", maxEpochDuration)
}

// PlanNextEpoch asks pick, during the first half of each epoch, for a duty in
// the next epoch, and returns the first one it accepts. Asking only in the
// first half leaves at least half an epoch to start the duplicate validator
// client, even for a duty in the next epoch's first slot.
func PlanNextEpoch[Duty any](
	ctx context.Context, client *beacon.Client, chain chaininfo.Info, pick func(epoch uint64) (Duty, bool),
) Duty {
	ginkgo.GinkgoHelper()
	for {
		now := time.Now()
		head := testsuite.MustSucceed(client.HeadSlot(ctx))
		epoch := max(chain.EpochAt(now), chain.Epoch(head))
		start := chain.SlotStartTime(epoch * chain.SlotsPerEpoch)
		if now.Before(start.Add(chain.EpochDuration / 2)) {
			if duty, ok := pick(epoch); ok {
				return duty
			}
		}
		gomega.Expect(testsuite.SleepUntil(ctx, start.Add(chain.EpochDuration))).To(gomega.Succeed())
	}
}

// AwaitSlashed reports whether the validator is slashed within
// detectionEpochs.
func AwaitSlashed(ctx context.Context, val rewards.Validator) bool {
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

// FindEvidence searches the blocks from the given slot up to the head, and
// returns the first one that slashes the validator, with its evidence.
func FindEvidence(ctx context.Context, val rewards.Validator, from uint64) (uint64, Evidence) {
	ginkgo.GinkgoHelper()
	head := testsuite.MustSucceed(val.Client.HeadSlot(ctx))
	for slot := from; slot <= head; slot++ {
		operations, err := val.Client.BlockOperations(ctx, strconv.FormatUint(slot, 10))
		if beacon.IsNotFound(err) {
			continue
		}
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		if evidence, found := EvidenceAgainst(operations, val.Index); found {
			return slot, evidence
		}
	}
	ginkgo.Fail("no block from slot " + strconv.FormatUint(from, 10) + " slashes the validator")
	return 0, Evidence{}
}
