package rewards

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// Spec holds the consensus constants of the base reward and of the
// inactivity leak.
type Spec struct {
	increment                    uint64
	baseRewardFactor             uint64
	sourceWeight                 uint64
	targetWeight                 uint64
	weightDenominator            uint64
	minEpochsToInactivityPenalty uint64
	minInclusionDelay            uint64
	inactivityScoreBias          uint64
	inactivityScoreRecoveryRate  uint64
	inactivityPenaltyQuotient    uint64
}

func LoadSpec(ctx context.Context, client *beacon.Client) (Spec, error) {
	values, err := client.Spec(ctx)
	if err != nil {
		return Spec{}, err
	}
	var spec Spec
	if err := values.Uints(map[string]*uint64{
		"EFFECTIVE_BALANCE_INCREMENT":      &spec.increment,
		"BASE_REWARD_FACTOR":               &spec.baseRewardFactor,
		"TIMELY_SOURCE_WEIGHT":             &spec.sourceWeight,
		"TIMELY_TARGET_WEIGHT":             &spec.targetWeight,
		"WEIGHT_DENOMINATOR":               &spec.weightDenominator,
		"MIN_EPOCHS_TO_INACTIVITY_PENALTY": &spec.minEpochsToInactivityPenalty,
		"MIN_ATTESTATION_INCLUSION_DELAY":  &spec.minInclusionDelay,
		"INACTIVITY_SCORE_BIAS":            &spec.inactivityScoreBias,
		"INACTIVITY_SCORE_RECOVERY_RATE":   &spec.inactivityScoreRecoveryRate,
		"INACTIVITY_PENALTY_QUOTIENT":      &spec.inactivityPenaltyQuotient,
	}); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

// missedVotePenalties returns what a validator with effectiveBalance loses
// for missing the source and the target vote of an epoch, in a leak or not.
// The inactivity penalty that a missed target adds is left out.
func (spec Spec) missedVotePenalties(effectiveBalance, totalActiveBalance uint64) (source, target uint64) {
	root := new(big.Int).Sqrt(new(big.Int).SetUint64(totalActiveBalance)).Uint64()
	baseReward := effectiveBalance / spec.increment * (spec.increment * spec.baseRewardFactor / root)
	return baseReward * spec.sourceWeight / spec.weightDenominator,
		baseReward * spec.targetWeight / spec.weightDenominator
}

// inactivityPenalty is what a validator with effectiveBalance and an
// inactivity score loses for missing an epoch's target vote, on top of the
// missed-target penalty.
func (spec Spec) inactivityPenalty(effectiveBalance, score uint64) uint64 {
	return effectiveBalance * score / (spec.inactivityScoreBias * spec.inactivityPenaltyQuotient)
}

// inInactivityLeak reports whether the rewards of epoch are processed in an
// inactivity leak while finalized is the last finalized epoch: whether the
// finality delay exceeds MIN_EPOCHS_TO_INACTIVITY_PENALTY.
func (spec Spec) inInactivityLeak(epoch, finalized uint64) bool {
	return epoch > finalized+spec.minEpochsToInactivityPenalty
}

// FirstLeakEpoch is the first epoch whose rewards are processed in an
// inactivity leak once finalized is the last finalized epoch.
func (spec Spec) FirstLeakEpoch(finalized uint64) uint64 {
	return finalized + spec.minEpochsToInactivityPenalty + 1
}

// NextInactivityScore is a validator's inactivity score after the epoch
// processing that counts its votes of an epoch, from its score before. A
// timely target vote takes one off, a missed one adds INACTIVITY_SCORE_BIAS;
// then, unless the epoch is processed in a leak, the score drops by up to
// INACTIVITY_SCORE_RECOVERY_RATE.
func (spec Spec) NextInactivityScore(score uint64, timelyTarget, leak bool) uint64 {
	if timelyTarget {
		score -= min(score, 1)
	} else {
		score += spec.inactivityScoreBias
	}
	if !leak {
		score -= min(score, spec.inactivityScoreRecoveryRate)
	}
	return score
}

// Validator reads one validator's balance and rewards from a beacon node.
type Validator struct {
	Client *beacon.Client
	Chain  chaininfo.Info
	Spec   Spec
	Index  uint64
}

// VoteAmounts is what a validator's votes in an epoch earn or cost. Timely is
// what each vote earns when it is timely: Qrysm's ideal reward at the
// validator's effective balance, zero in an inactivity leak. MissedSource and
// MissedTarget are what missing those votes costs; missing the head vote
// costs nothing.
type VoteAmounts struct {
	Timely       beacon.IdealAttestationReward
	MissedSource uint64
	MissedTarget uint64
}

// AwaitRewards waits until Qrysm serves the attestation rewards of epoch, two
// epochs later. Block and sync committee rewards need no wait: Qrysm serves
// them once their block exists.
func (validator Validator) AwaitRewards(ctx context.Context, epoch uint64) {
	ginkgo.GinkgoHelper()
	head := testsuite.MustSucceed(validator.Client.HeadSlot(ctx))
	remaining := max(int64(epoch+2)-int64(validator.Chain.Epoch(head)), 0)
	gomega.Eventually(func(g gomega.Gomega) {
		head, err := validator.Client.HeadSlot(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(rewardsAvailable(validator.Chain, head, epoch)).To(gomega.BeTrue())
	}).WithContext(ctx).
		WithTimeout(time.Duration(remaining+2)*validator.Chain.EpochDuration).
		WithPolling(PollInterval).
		Should(gomega.Succeed(), "the rewards of epoch %d are not available", epoch)
}

// AttestationReward returns the validator's attestation reward for epoch.
func (validator Validator) AttestationReward(ctx context.Context, epoch uint64) (beacon.AttestationReward, error) {
	return AttestationReward(ctx, validator.Client, epoch, validator.Index)
}

// FirstAttestedEpoch waits for the first epoch, starting at epoch from, whose
// source and target votes were both timely, and returns it.
func (validator Validator) FirstAttestedEpoch(ctx context.Context, from uint64, timeout time.Duration) uint64 {
	ginkgo.GinkgoHelper()
	return AwaitAttestationReward(ctx, validator.Client, validator.Chain, validator.Index, from, timeout, attested)
}

func attested(reward beacon.AttestationReward) bool {
	return reward.Source > 0 && reward.Target > 0
}

// VoteAmounts computes the vote amounts of epoch from the state its rewards
// are processed in: the one at the last slot of the next epoch. It refuses an
// epoch processed in an inactivity leak, whose penalty Qrysm folds into the
// target score; LeakVoteAmounts takes one.
func (validator Validator) VoteAmounts(ctx context.Context, epoch uint64) (VoteAmounts, error) {
	stateID := strconv.FormatUint(validator.rewardsSlot(epoch), 10)
	checkpoints, err := validator.Client.FinalityCheckpointsAt(ctx, stateID)
	if err != nil {
		return VoteAmounts{}, err
	}
	if finalized := checkpoints.Finalized.Epoch; validator.Spec.inInactivityLeak(epoch, finalized) {
		return VoteAmounts{}, fmt.Errorf(
			"the rewards of epoch %d are processed in an inactivity leak: the last finalized epoch is %d", epoch, finalized)
	}
	return validator.voteAmountsAt(ctx, epoch, stateID)
}

// LeakVoteAmounts are the vote amounts of an epoch that may be processed in an
// inactivity leak, or while the validator's inactivity score is above zero.
type LeakVoteAmounts struct {
	VoteAmounts
	// Leak is whether the epoch processing that counts the epoch's votes does
	// so in a leak, by the finalized checkpoint after its justification step.
	// ReportedLeak is the rewards API's view, by the checkpoint before that
	// step. They differ only in a processing that finalizes, where Timely,
	// zero in a reported leak, is not what timely votes earn.
	Leak, ReportedLeak bool
	// ScoreBefore and ScoreAfter are the validator's inactivity score before
	// and after that epoch processing.
	ScoreBefore, ScoreAfter uint64
	// Inactivity is what that epoch processing charges on top of MissedTarget
	// when the target vote was missed, at ScoreAfter. ReportedInactivity is
	// what the rewards API adds to the target figure instead: it skips the
	// processing's score update and charges ScoreBefore. The two Reported
	// fields follow the API until theQRL/qrysm#112 is fixed.
	Inactivity, ReportedInactivity uint64
}

// LeakVoteAmounts computes the vote amounts of epoch like VoteAmounts, and the
// inactivity penalties of a missed target vote, from the states just before
// and just after the epoch processing that counts the epoch's votes.
func (validator Validator) LeakVoteAmounts(ctx context.Context, epoch uint64) (LeakVoteAmounts, error) {
	before := validator.rewardsSlot(epoch)
	amounts, err := validator.voteAmountsAt(ctx, epoch, strconv.FormatUint(before, 10))
	if err != nil {
		return LeakVoteAmounts{}, err
	}
	leak := LeakVoteAmounts{VoteAmounts: amounts}
	if leak.ReportedLeak, leak.ScoreBefore, err = validator.leakAndScore(ctx, epoch, before); err != nil {
		return LeakVoteAmounts{}, err
	}
	if leak.Leak, leak.ScoreAfter, err = validator.leakAndScore(ctx, epoch, before+1); err != nil {
		return LeakVoteAmounts{}, err
	}
	effectiveBalance := amounts.Timely.EffectiveBalance
	leak.Inactivity = validator.Spec.inactivityPenalty(effectiveBalance, leak.ScoreAfter)
	leak.ReportedInactivity = validator.Spec.inactivityPenalty(effectiveBalance, leak.ScoreBefore)
	return leak, nil
}

// leakAndScore reads, from the state at slot, whether the rewards of epoch
// count as processed in a leak there, and the validator's inactivity score.
func (validator Validator) leakAndScore(ctx context.Context, epoch, slot uint64) (bool, uint64, error) {
	stateID := strconv.FormatUint(slot, 10)
	checkpoints, err := validator.Client.FinalityCheckpointsAt(ctx, stateID)
	if err != nil {
		return false, 0, err
	}
	scores, err := validator.Client.InactivityScores(ctx, stateID)
	if err != nil {
		return false, 0, err
	}
	if validator.Index >= uint64(len(scores)) {
		return false, 0, fmt.Errorf("the state at slot %d has no inactivity score for validator %d", slot, validator.Index)
	}
	return validator.Spec.inInactivityLeak(epoch, checkpoints.Finalized.Epoch), scores[validator.Index], nil
}

// rewardsSlot is the slot of the state the rewards of epoch are processed in:
// the last of the next epoch.
func (validator Validator) rewardsSlot(epoch uint64) uint64 {
	return (epoch+2)*validator.Chain.SlotsPerEpoch - 1
}

func (validator Validator) voteAmountsAt(ctx context.Context, epoch uint64, stateID string) (VoteAmounts, error) {
	record, err := validator.Client.ValidatorAt(ctx, stateID, strconv.FormatUint(validator.Index, 10))
	if err != nil {
		return VoteAmounts{}, err
	}
	total, err := validator.Client.TotalActiveBalance(ctx, stateID)
	if err != nil {
		return VoteAmounts{}, err
	}
	ideal, err := validator.Client.IdealAttestationRewards(ctx, epoch, []uint64{validator.Index})
	if err != nil {
		return VoteAmounts{}, err
	}
	amounts := VoteAmounts{}
	amounts.MissedSource, amounts.MissedTarget = validator.Spec.missedVotePenalties(record.EffectiveBalance, total)
	for _, reward := range ideal {
		if reward.EffectiveBalance == record.EffectiveBalance {
			amounts.Timely = reward
			return amounts, nil
		}
	}
	return VoteAmounts{}, fmt.Errorf("epoch %d has no ideal reward for effective balance %d", epoch, record.EffectiveBalance)
}

// BalanceAtEpochStart is the balance at the first slot of epoch, after that
// slot's block. It includes the attestation rewards of epoch-2, which the
// processing at the start of epoch applies.
func (validator Validator) BalanceAtEpochStart(ctx context.Context, epoch uint64) (uint64, error) {
	return validator.BalanceAt(ctx, epoch*validator.Chain.SlotsPerEpoch)
}

// BalanceAt is the balance in the state at slot.
func (validator Validator) BalanceAt(ctx context.Context, slot uint64) (uint64, error) {
	record, err := validator.RecordAt(ctx, slot)
	return record.Balance, err
}

// RecordAtEpochStart is the validator's record in the state at the first slot
// of epoch.
func (validator Validator) RecordAtEpochStart(ctx context.Context, epoch uint64) (beacon.Validator, error) {
	return validator.RecordAt(ctx, epoch*validator.Chain.SlotsPerEpoch)
}

// RecordAt is the validator's record in the state at slot.
func (validator Validator) RecordAt(ctx context.Context, slot uint64) (beacon.Validator, error) {
	return validator.Client.ValidatorAt(ctx, strconv.FormatUint(slot, 10), strconv.FormatUint(validator.Index, 10))
}

// BlockBalanceChanges sums BlockBalanceChange over the blocks between
// BalanceAtEpochStart of epoch and of the next: the second slot of epoch
// through the first slot of the next.
func (validator Validator) BlockBalanceChanges(ctx context.Context, epoch uint64) (int64, error) {
	var change int64
	start := epoch * validator.Chain.SlotsPerEpoch
	for slot := start + 1; slot <= start+validator.Chain.SlotsPerEpoch; slot++ {
		blockChange, err := validator.BlockBalanceChange(ctx, slot)
		if err != nil {
			return 0, err
		}
		change += blockChange
	}
	return change, nil
}

// BlockBalanceChange is what the block at slot did to the validator's balance,
// zero for an empty slot: its reward for proposing the block, its sync
// committee reward (negative for a missed duty), less its withdrawals. A
// slashing the block includes is left to the caller.
func (validator Validator) BlockBalanceChange(ctx context.Context, slot uint64) (int64, error) {
	blockID := strconv.FormatUint(slot, 10)
	operations, err := validator.Client.BlockOperations(ctx, blockID)
	if beacon.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var change int64
	if operations.ProposerIndex == validator.Index {
		proposed, err := validator.Client.BlockRewards(ctx, blockID)
		if err != nil {
			return 0, err
		}
		change += int64(proposed.Total)
	}
	for _, withdrawal := range operations.Withdrawals {
		if withdrawal.ValidatorIndex == validator.Index {
			change -= int64(withdrawal.Amount)
		}
	}
	rewards, err := validator.Client.SyncCommitteeRewards(ctx, blockID, []uint64{validator.Index})
	if err != nil {
		return 0, err
	}
	for _, reward := range rewards {
		change += reward.Reward
	}
	return change, nil
}

// TimelyVotes is whether an attestation's source and head votes were timely.
// Target is left out: a correct target is timely at any delay.
type TimelyVotes struct {
	Source bool
	Head   bool
}

// TimelyVotes reads, from the blocks that include the validator's attestation
// of epoch, whether its source and head votes were timely. Source must be
// included within ⌊√SLOTS_PER_EPOCH⌋ slots; head after exactly
// MIN_ATTESTATION_INCLUSION_DELAY slots, voting the canonical head and target.
func (validator Validator) TimelyVotes(ctx context.Context, epoch uint64) (TimelyVotes, error) {
	start := epoch * validator.Chain.SlotsPerEpoch
	committees, err := validator.Client.Committees(ctx, strconv.FormatUint(start, 10), epoch)
	if err != nil {
		return TimelyVotes{}, err
	}
	slot, committee, position, found := validator.duty(committees)
	if !found {
		return TimelyVotes{}, fmt.Errorf("validator %d is in no committee of epoch %d", validator.Index, epoch)
	}
	headRoot, err := validator.rootAtOrBefore(ctx, slot)
	if err != nil {
		return TimelyVotes{}, err
	}
	targetRoot, err := validator.rootAtOrBefore(ctx, start)
	if err != nil {
		return TimelyVotes{}, err
	}

	sourceWindow := new(big.Int).Sqrt(new(big.Int).SetUint64(validator.Chain.SlotsPerEpoch)).Uint64()
	var votes TimelyVotes
	for delay := validator.Spec.minInclusionDelay; delay <= sourceWindow; delay++ {
		operations, err := validator.Client.BlockOperations(ctx, strconv.FormatUint(slot+delay, 10))
		if beacon.IsNotFound(err) {
			continue
		}
		if err != nil {
			return TimelyVotes{}, err
		}
		for _, attestation := range operations.Attestations {
			data := attestation.Data
			if data.Slot != slot || data.CommitteeIndex != committee || !attestation.Includes(position) {
				continue
			}
			votes.Source = true
			if delay == validator.Spec.minInclusionDelay &&
				strings.EqualFold(data.BeaconBlockRoot, headRoot) && strings.EqualFold(data.Target.Root, targetRoot) {
				votes.Head = true
			}
		}
	}
	return votes, nil
}

// duty finds the validator's attestation duty among committees.
func (validator Validator) duty(committees []beacon.Committee) (slot, committee, position uint64, found bool) {
	for _, candidate := range committees {
		for member, index := range candidate.Validators {
			if index == validator.Index {
				return candidate.Slot, candidate.Index, uint64(member), true
			}
		}
	}
	return 0, 0, 0, false
}

// rootAtOrBefore returns the root of the canonical block at slot, or of the
// last one before it if the slot is empty.
func (validator Validator) rootAtOrBefore(ctx context.Context, slot uint64) (string, error) {
	for {
		header, err := validator.Client.BlockHeader(ctx, strconv.FormatUint(slot, 10))
		if err == nil {
			return header.Root, nil
		}
		if !beacon.IsNotFound(err) || slot == 0 {
			return "", err
		}
		slot--
	}
}
