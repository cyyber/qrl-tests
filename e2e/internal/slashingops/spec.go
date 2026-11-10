package slashingops

import (
	"context"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
)

// Spec holds the consensus constants of a slashing and of the exit it forces.
type Spec struct {
	minSlashingPenaltyQuotient  uint64
	whistleblowerRewardQuotient uint64
	epochsPerSlashingsVector    uint64
	maxSeedLookahead            uint64
	minWithdrawabilityDelay     uint64
}

func LoadSpec(ctx context.Context, client *beacon.Client) (Spec, error) {
	values, err := client.Spec(ctx)
	if err != nil {
		return Spec{}, err
	}
	var spec Spec
	if err := values.Uints(map[string]*uint64{
		"MIN_SLASHING_PENALTY_QUOTIENT":       &spec.minSlashingPenaltyQuotient,
		"WHISTLEBLOWER_REWARD_QUOTIENT":       &spec.whistleblowerRewardQuotient,
		"EPOCHS_PER_SLASHINGS_VECTOR":         &spec.epochsPerSlashingsVector,
		"MAX_SEED_LOOKAHEAD":                  &spec.maxSeedLookahead,
		"MIN_VALIDATOR_WITHDRAWABILITY_DELAY": &spec.minWithdrawabilityDelay,
	}); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

// InitialPenalty is what a validator with effectiveBalance loses in the block
// that slashes it.
func (spec Spec) InitialPenalty(effectiveBalance uint64) uint64 {
	return effectiveBalance / spec.minSlashingPenaltyQuotient
}

// WhistleblowerReward is what the proposer of the block that slashes a
// validator with effectiveBalance earns.
func (spec Spec) WhistleblowerReward(effectiveBalance uint64) uint64 {
	return effectiveBalance / spec.whistleblowerRewardQuotient
}

// ExitEpoch is the exit epoch a slashing in epoch assigns when no other exit
// is queued: the earliest epoch any exit can take effect.
func (spec Spec) ExitEpoch(epoch uint64) uint64 {
	return epoch + 1 + spec.maxSeedLookahead
}

// WithdrawableEpoch is when a validator slashed in epoch, exiting at exit,
// can withdraw: the later of its exit plus the withdrawability delay, and a
// whole slashings vector after the slashing, when its correlation penalty is
// due.
func (spec Spec) WithdrawableEpoch(epoch, exit uint64) uint64 {
	return max(exit+spec.minWithdrawabilityDelay, epoch+spec.epochsPerSlashingsVector)
}
