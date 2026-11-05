//go:build e2e

package slashing

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

const detectionEpochs = 4

// suiteSlashed is every validator the suite's scenarios have slashed. Ginkgo
// may run the scenarios in either order on the same network, so each checks
// that no validator outside this list is slashed.
var suiteSlashed []uint64

// awaitSlashed reports whether the validator is slashed within
// detectionEpochs, and records it in suiteSlashed if it is.
func awaitSlashed(ctx context.Context, val rewards.Validator) bool {
	ginkgo.GinkgoHelper()
	deadline := time.Now().Add(detectionEpochs * val.Chain.EpochDuration)
	index := strconv.FormatUint(val.Index, 10)
	for time.Now().Before(deadline) {
		record := testsuite.MustSucceed(val.Client.Validator(ctx, index))
		if record.Slashed {
			suiteSlashed = append(suiteSlashed, val.Index)
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

// expectOnlySuiteSlashings checks that the chain has slashed exactly the
// validators in suiteSlashed, and that it keeps finalizing.
func expectOnlySuiteSlashings(ctx context.Context, client *beacon.Client, chain chaininfo.Info) {
	ginkgo.GinkgoHelper()
	slashed := testsuite.MustSucceed(client.SlashedValidators(ctx))
	indices := make([]uint64, len(slashed))
	for position, record := range slashed {
		indices[position] = record.Index
	}
	gomega.Expect(indices).To(gomega.ConsistOf(suiteSlashed))

	head := testsuite.MustSucceed(client.HeadSlot(ctx))
	checkpoints := testsuite.MustSucceed(client.FinalityCheckpoints(ctx))
	gomega.Expect(checkpoints.Finalized.Epoch+maxFinalityLagEpochs).To(gomega.BeNumerically(">=", chain.Epoch(head)),
		"finalized epoch %d at head slot %d", checkpoints.Finalized.Epoch, head)
}

func slotStart(chain chaininfo.Info, slot uint64) time.Time {
	return chain.GenesisTime.Add(time.Duration(slot) * chain.SlotDuration)
}
