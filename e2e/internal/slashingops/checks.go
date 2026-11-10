package slashingops

import (
	"context"
	"strconv"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// ExpectSlashedInBlock checks the block at slot that slashes the validator for
// offense: it pays the initial penalty, sets the exit and withdrawable epochs,
// and pays its proposer the whistleblower reward for that kind of evidence. It
// returns the validator's record at the block.
func ExpectSlashedInBlock(
	ctx context.Context, val rewards.Validator, spec Spec, slot uint64, offense Offense,
) beacon.Validator {
	ginkgo.GinkgoHelper()
	before := testsuite.MustSucceed(val.RecordAt(ctx, slot-1))
	on := testsuite.MustSucceed(val.RecordAt(ctx, slot))
	gomega.Expect(before.Slashed).To(gomega.BeFalse())
	gomega.Expect(on.Slashed).To(gomega.BeTrue())
	gomega.Expect(on.Status).To(gomega.Equal("active_slashed"))

	// The block's own duties change the balance too, and a block at an
	// epoch's first slot also follows epoch processing, which applies the
	// attestation rewards of epoch-2.
	epoch := val.Chain.Epoch(slot)
	penalty := spec.InitialPenalty(before.EffectiveBalance)
	blockChange := testsuite.MustSucceed(val.BlockBalanceChange(ctx, slot))
	var epochChange int64
	if slot%val.Chain.SlotsPerEpoch == 0 {
		reward := testsuite.MustSucceed(val.AttestationReward(ctx, epoch-2))
		epochChange = reward.Head + reward.Source + reward.Target
	}
	want := int64(before.Balance) - int64(penalty) + blockChange + epochChange
	gomega.Expect(int64(on.Balance)).To(gomega.Equal(want),
		"balance at slot %d is %d, want %d: %d before, minus the %d shor penalty, plus %d shor from the block's duties and %d from epoch processing",
		slot, on.Balance, want, before.Balance, penalty, blockChange, epochChange)

	exit := spec.ExitEpoch(epoch)
	gomega.Expect(on.ExitEpoch).To(gomega.Equal(exit), "exit epoch of a validator slashed in epoch %d", epoch)
	gomega.Expect(on.WithdrawableEpoch).To(gomega.Equal(spec.WithdrawableEpoch(epoch, exit)),
		"withdrawable epoch of a validator slashed in epoch %d", epoch)

	blockRewards := testsuite.MustSucceed(val.Client.BlockRewards(ctx, strconv.FormatUint(slot, 10)))
	reward := spec.WhistleblowerReward(before.EffectiveBalance)
	wantProposerSlashings, wantAttesterSlashings := uint64(0), reward
	if offense == DoubleProposal {
		wantProposerSlashings, wantAttesterSlashings = reward, 0
	}
	gomega.Expect(blockRewards.ProposerSlashings).To(gomega.Equal(wantProposerSlashings),
		"what the block's proposer earned for proposer slashings")
	gomega.Expect(blockRewards.AttesterSlashings).To(gomega.Equal(wantAttesterSlashings),
		"what the block's proposer earned for attester slashings")
	return on
}

// ExpectDoubleVoteEvidence checks that the evidence the block at slot holds is
// an attester slashing for a double vote, and returns its two votes.
func ExpectDoubleVoteEvidence(slot uint64, evidence Evidence) (first, second beacon.AttestationData) {
	ginkgo.GinkgoHelper()
	gomega.Expect(evidence.Attester).NotTo(gomega.BeNil(),
		"the block at slot %d slashed the validator for a double proposal", slot)
	first, second = evidence.Attester.Attestation1.Data, evidence.Attester.Attestation2.Data
	gomega.Expect(IsDoubleVote(first, second)).To(gomega.BeTrue(),
		"the attestations of slots %d and %d are not a double vote", first.Slot, second.Slot)
	return first, second
}

// ExpectDoubleProposalEvidence checks that the evidence the block at slot
// holds is a proposer slashing for a double proposal, and returns its two
// headers.
func ExpectDoubleProposalEvidence(slot uint64, evidence Evidence) (first, second beacon.BeaconBlockHeaderMessage) {
	ginkgo.GinkgoHelper()
	gomega.Expect(evidence.Proposer).NotTo(gomega.BeNil(),
		"the block at slot %d slashed the validator for a double vote", slot)
	first, second = evidence.Proposer.Header1, evidence.Proposer.Header2
	gomega.Expect(IsDoubleProposal(first, second)).To(gomega.BeTrue(),
		"the headers of slots %d and %d are not a double proposal", first.Slot, second.Slot)
	return first, second
}

// ExpectOnlySlashed checks that every slashed validator is one of allowed.
func ExpectOnlySlashed(ctx context.Context, client *beacon.Client, allowed []uint64) {
	ginkgo.GinkgoHelper()
	slashed := testsuite.MustSucceed(client.SlashedValidators(ctx))
	indices := make([]uint64, len(slashed))
	for position, record := range slashed {
		indices[position] = record.Index
	}
	gomega.Expect(indices).To(gomega.HaveEach(gomega.BeElementOf(allowed)),
		"slashed validators, where only %v may be", allowed)
}
