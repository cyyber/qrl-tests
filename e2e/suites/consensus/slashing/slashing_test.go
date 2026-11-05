package slashing

import (
	"cmp"
	"context"
	"slices"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
)

// slashingSpec holds the consensus constants of a slashing and of the exit
// it forces.
type slashingSpec struct {
	minSlashingPenaltyQuotient  uint64
	whistleblowerRewardQuotient uint64
	epochsPerSlashingsVector    uint64
	maxSeedLookahead            uint64
	minWithdrawabilityDelay     uint64
}

func loadSlashingSpec(ctx context.Context, client *beacon.Client) (slashingSpec, error) {
	values, err := client.Spec(ctx)
	if err != nil {
		return slashingSpec{}, err
	}
	var spec slashingSpec
	if err := values.Uints(map[string]*uint64{
		"MIN_SLASHING_PENALTY_QUOTIENT":       &spec.minSlashingPenaltyQuotient,
		"WHISTLEBLOWER_REWARD_QUOTIENT":       &spec.whistleblowerRewardQuotient,
		"EPOCHS_PER_SLASHINGS_VECTOR":         &spec.epochsPerSlashingsVector,
		"MAX_SEED_LOOKAHEAD":                  &spec.maxSeedLookahead,
		"MIN_VALIDATOR_WITHDRAWABILITY_DELAY": &spec.minWithdrawabilityDelay,
	}); err != nil {
		return slashingSpec{}, err
	}
	return spec, nil
}

// initialPenalty is what a validator with effectiveBalance loses in the block
// that slashes it.
func (spec slashingSpec) initialPenalty(effectiveBalance uint64) uint64 {
	return effectiveBalance / spec.minSlashingPenaltyQuotient
}

// whistleblowerReward is what the proposer of the block that slashes a
// validator with effectiveBalance earns.
func (spec slashingSpec) whistleblowerReward(effectiveBalance uint64) uint64 {
	return effectiveBalance / spec.whistleblowerRewardQuotient
}

// exitEpoch is the exit epoch a slashing in epoch assigns when no other exit
// is queued: the earliest epoch any exit can take effect.
func (spec slashingSpec) exitEpoch(epoch uint64) uint64 {
	return epoch + 1 + spec.maxSeedLookahead
}

// withdrawableEpoch is when a validator slashed in epoch, exiting at exit,
// can withdraw: the later of its exit plus the withdrawability delay, and a
// whole slashings vector after the slashing, when its correlation penalty is
// due.
func (spec slashingSpec) withdrawableEpoch(epoch, exit uint64) uint64 {
	return max(exit+spec.minWithdrawabilityDelay, epoch+spec.epochsPerSlashingsVector)
}

// isDoubleVote reports whether the two attestations are different votes for
// the same target epoch, which is slashable.
func isDoubleVote(first, second beacon.AttestationData) bool {
	return first != second && first.Target.Epoch == second.Target.Epoch
}

// proposalCandidates returns the proposals of the next epoch that a duplicate
// of their proposer's key can turn into a double proposal and nothing else:
// the proposer is one of candidates, not one of excluded, and proposes no
// other slot in the current epoch or the next. The latest come first, to give
// the duplicate the most time to start.
func proposalCandidates(current, next []beacon.ProposerDuty, candidates, excluded []uint64) []beacon.ProposerDuty {
	proposals := make(map[uint64]int)
	for _, duty := range slices.Concat(current, next) {
		proposals[duty.ValidatorIndex]++
	}
	var chosen []beacon.ProposerDuty
	for _, duty := range next {
		if slices.Contains(candidates, duty.ValidatorIndex) && !slices.Contains(excluded, duty.ValidatorIndex) &&
			proposals[duty.ValidatorIndex] == 1 {
			chosen = append(chosen, duty)
		}
	}
	slices.SortFunc(chosen, func(first, second beacon.ProposerDuty) int { return cmp.Compare(second.Slot, first.Slot) })
	return chosen
}

// isDoubleProposal reports whether the two headers are different blocks that
// one proposer signed for the same slot.
func isDoubleProposal(first, second beacon.BeaconBlockHeaderMessage) bool {
	return first != second && first.Slot == second.Slot && first.ProposerIndex == second.ProposerIndex
}

// slashingEvidence is a block's evidence against one validator.
type slashingEvidence struct {
	attester *beacon.AttesterSlashing
	proposer *beacon.ProposerSlashing
}

// evidenceAgainst returns the evidence in a block that slashes the validator
// at index.
func evidenceAgainst(operations beacon.BlockOperations, index uint64) (slashingEvidence, bool) {
	for position := range operations.AttesterSlashings {
		slashing := &operations.AttesterSlashings[position]
		if slices.Contains(slashing.Attestation1.AttestingIndices, index) &&
			slices.Contains(slashing.Attestation2.AttestingIndices, index) {
			return slashingEvidence{attester: slashing}, true
		}
	}
	for position := range operations.ProposerSlashings {
		slashing := &operations.ProposerSlashings[position]
		if slashing.Header1.ProposerIndex == index {
			return slashingEvidence{proposer: slashing}, true
		}
	}
	return slashingEvidence{}, false
}
