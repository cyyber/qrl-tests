//go:build e2e

package slashing

import (
	"context"
	"strconv"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// expectSlashedInBlock checks the block at slot that slashes the validator: it
// pays the initial penalty, sets the exit and withdrawable epochs, and pays its
// proposer the whistleblower reward for a proposer slashing if doubleProposal,
// or an attester slashing if not. It returns the validator's record at the
// block.
func expectSlashedInBlock(
	ctx context.Context, val rewards.Validator, spec slashingSpec, slot uint64, doubleProposal bool,
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
	penalty := spec.initialPenalty(before.EffectiveBalance)
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

	exit := spec.exitEpoch(epoch)
	gomega.Expect(on.ExitEpoch).To(gomega.Equal(exit), "exit epoch of a validator slashed in epoch %d", epoch)
	gomega.Expect(on.WithdrawableEpoch).To(gomega.Equal(spec.withdrawableEpoch(epoch, exit)),
		"withdrawable epoch of a validator slashed in epoch %d", epoch)

	blockRewards := testsuite.MustSucceed(val.Client.BlockRewards(ctx, strconv.FormatUint(slot, 10)))
	reward := spec.whistleblowerReward(before.EffectiveBalance)
	wantProposerSlashings, wantAttesterSlashings := uint64(0), reward
	if doubleProposal {
		wantProposerSlashings, wantAttesterSlashings = reward, 0
	}
	gomega.Expect(blockRewards.ProposerSlashings).To(gomega.Equal(wantProposerSlashings),
		"what the block's proposer earned for proposer slashings")
	gomega.Expect(blockRewards.AttesterSlashings).To(gomega.Equal(wantAttesterSlashings),
		"what the block's proposer earned for attester slashings")
	return on
}

// expectDoubleVoteEvidence checks that the evidence the block at slot holds is
// an attester slashing for a double vote, and returns its two votes.
func expectDoubleVoteEvidence(slot uint64, evidence slashingEvidence) (first, second beacon.AttestationData) {
	ginkgo.GinkgoHelper()
	gomega.Expect(evidence.attester).NotTo(gomega.BeNil(),
		"the block at slot %d slashed the validator for a double proposal", slot)
	first, second = evidence.attester.Attestation1.Data, evidence.attester.Attestation2.Data
	gomega.Expect(isDoubleVote(first, second)).To(gomega.BeTrue(),
		"the attestations of slots %d and %d are not a double vote", first.Slot, second.Slot)
	return first, second
}

// expectDoubleProposalEvidence checks that the evidence the block at slot holds
// is a proposer slashing for a double proposal, and returns its two headers.
func expectDoubleProposalEvidence(slot uint64, evidence slashingEvidence) (first, second beacon.BeaconBlockHeaderMessage) {
	ginkgo.GinkgoHelper()
	gomega.Expect(evidence.proposer).NotTo(gomega.BeNil(),
		"the block at slot %d slashed the validator for a double vote", slot)
	first, second = evidence.proposer.Header1, evidence.proposer.Header2
	gomega.Expect(isDoubleProposal(first, second)).To(gomega.BeTrue(),
		"the headers of slots %d and %d are not a double proposal", first.Slot, second.Slot)
	return first, second
}

// expectOnlyDuplicatesSlashed checks that every slashed validator is one whose
// key the suite ran in a second client, and that the chain keeps finalizing.
func expectOnlyDuplicatesSlashed(ctx context.Context, client *beacon.Client, chain chaininfo.Info) {
	ginkgo.GinkgoHelper()
	slashed := testsuite.MustSucceed(client.SlashedValidators(ctx))
	indices := make([]uint64, len(slashed))
	for position, record := range slashed {
		indices[position] = record.Index
	}
	gomega.Expect(indices).To(gomega.HaveEach(gomega.BeElementOf(suiteDuplicated)),
		"slashed validators, where only the duplicated %v may be", suiteDuplicated)

	head := testsuite.MustSucceed(client.HeadSlot(ctx))
	checkpoints := testsuite.MustSucceed(client.FinalityCheckpoints(ctx))
	gomega.Expect(checkpoints.Finalized.Epoch+maxFinalityLagEpochs).To(gomega.BeNumerically(">=", chain.Epoch(head)),
		"finalized epoch %d at head slot %d", checkpoints.Finalized.Epoch, head)
}

// expectExitedSlashed checks the validator's record at the start of epoch: it
// has exited as slashed, and the withdrawable epoch set when it was slashed is
// still ahead.
func expectExitedSlashed(ctx context.Context, val rewards.Validator, epoch uint64, onSlashing beacon.Validator) {
	ginkgo.GinkgoHelper()
	record := testsuite.MustSucceed(val.RecordAtEpochStart(ctx, epoch))
	gomega.Expect(record.Status).To(gomega.Equal("exited_slashed"), "status at the start of epoch %d", epoch)
	gomega.Expect(record.WithdrawableEpoch).To(gomega.Equal(onSlashing.WithdrawableEpoch),
		"withdrawable epoch at the start of epoch %d", epoch)
	gomega.Expect(epoch).To(gomega.BeNumerically("<", record.WithdrawableEpoch),
		"the validator can withdraw at the start of epoch %d", epoch)
}

// expectMissedVotePenalty checks that the validator's votes of epoch cost
// exactly the missed source and target penalties, and that its balance moved
// by that and its block duties alone.
func expectMissedVotePenalty(ctx context.Context, val rewards.Validator, epoch uint64) {
	ginkgo.GinkgoHelper()
	reward := testsuite.MustSucceed(val.AttestationReward(ctx, epoch))
	amounts := testsuite.MustSucceed(val.VoteAmounts(ctx, epoch))
	gomega.Expect(reward).To(gomega.Equal(beacon.AttestationReward{
		ValidatorIndex: val.Index,
		Source:         -int64(amounts.MissedSource),
		Target:         -int64(amounts.MissedTarget),
	}), "attestation rewards of epoch %d", epoch)

	before := testsuite.MustSucceed(val.BalanceAtEpochStart(ctx, epoch+1))
	after := testsuite.MustSucceed(val.BalanceAtEpochStart(ctx, epoch+2))
	blocksChange := testsuite.MustSucceed(val.BlockBalanceChanges(ctx, epoch+1))
	penalty := amounts.MissedSource + amounts.MissedTarget
	want := int64(before) - int64(penalty) + blocksChange
	gomega.Expect(int64(after)).To(gomega.Equal(want),
		"balance at the start of epoch %d is %d, want %d: %d before, minus the %d shor penalty of epoch %d, plus %d shor from blocks",
		epoch+2, after, want, before, penalty, epoch, blocksChange)
}
