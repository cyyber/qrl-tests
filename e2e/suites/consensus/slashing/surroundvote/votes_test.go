//go:build e2e

package surroundvote

import (
	"context"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/slashingops"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// attesterDuty returns the validator's committee seat in epoch, which must be
// the current epoch or the next.
func attesterDuty(ctx context.Context, client *beacon.Client, epoch, index uint64) beacon.AttesterDuty {
	ginkgo.GinkgoHelper()
	duties := testsuite.MustSucceed(client.AttesterDuties(ctx, epoch, []uint64{index}))
	gomega.Expect(duties).To(gomega.HaveLen(1), "attester duties of validator %d in epoch %d", index, epoch)
	return duties[0]
}

// awaitVote waits until a third into the duty's slot, when a validator votes:
// gossip ignores votes for later slots, and the slot's block has arrived.
func awaitVote(ctx context.Context, chain chaininfo.Info, duty beacon.AttesterDuty) {
	ginkgo.GinkgoHelper()
	gomega.Expect(testsuite.SleepUntil(ctx, chain.SlotStartTime(duty.Slot).Add(chain.SlotDuration/3))).To(gomega.Succeed())
}

// headVote is the duty's vote for the canonical head at its slot and its
// epoch's checkpoint, from source. Gossip requires the target to be the head's
// ancestor at the epoch's start.
func headVote(
	ctx context.Context, client *beacon.Client, chain chaininfo.Info, duty beacon.AttesterDuty, source beacon.Checkpoint,
) beacon.AttestationData {
	ginkgo.GinkgoHelper()
	epoch := chain.Epoch(duty.Slot)
	return beacon.AttestationData{
		Slot:            duty.Slot,
		CommitteeIndex:  duty.CommitteeIndex,
		BeaconBlockRoot: testsuite.MustSucceed(client.RootAtOrBefore(ctx, duty.Slot)),
		Source:          source,
		Target: beacon.Checkpoint{
			Epoch: epoch,
			Root:  testsuite.MustSucceed(client.RootAtOrBefore(ctx, epoch*chain.SlotsPerEpoch)),
		},
	}
}

// publish signs the vote and hands it to the node, which publishes it on its
// committee's subnet.
func publish(
	ctx context.Context, client *beacon.Client, signer validatorops.Signer, duty beacon.AttesterDuty, data beacon.AttestationData,
) {
	ginkgo.GinkgoHelper()
	attestation := testsuite.MustSucceed(slashingops.SignAttestation(signer, duty, data))
	ginkgo.GinkgoWriter.Printf("publishing %+v with aggregation bits %s\n", data, attestation.AggregationBits)
	gomega.Expect(client.SubmitAttestations(ctx, []beacon.SignedAttestation{attestation})).To(gomega.Succeed())
}
