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

// Spec holds the consensus constants of the base reward.
type Spec struct {
	increment                    uint64
	baseRewardFactor             uint64
	sourceWeight                 uint64
	targetWeight                 uint64
	weightDenominator            uint64
	minEpochsToInactivityPenalty uint64
	minInclusionDelay            uint64
}

func LoadSpec(ctx context.Context, client *beacon.Client) (Spec, error) {
	values, err := client.Spec(ctx)
	if err != nil {
		return Spec{}, err
	}
	var spec Spec
	for _, constant := range []struct {
		name  string
		field *uint64
	}{
		{"EFFECTIVE_BALANCE_INCREMENT", &spec.increment},
		{"BASE_REWARD_FACTOR", &spec.baseRewardFactor},
		{"TIMELY_SOURCE_WEIGHT", &spec.sourceWeight},
		{"TIMELY_TARGET_WEIGHT", &spec.targetWeight},
		{"WEIGHT_DENOMINATOR", &spec.weightDenominator},
		{"MIN_EPOCHS_TO_INACTIVITY_PENALTY", &spec.minEpochsToInactivityPenalty},
		{"MIN_ATTESTATION_INCLUSION_DELAY", &spec.minInclusionDelay},
	} {
		value, err := values.Uint(constant.name)
		if err != nil {
			return Spec{}, err
		}
		*constant.field = value
	}
	return spec, nil
}

// missedVotePenalties returns what a validator with effectiveBalance loses
// for missing the source and the target vote of an epoch outside an
// inactivity leak. Its inactivity score then stays zero and adds nothing.
func (spec Spec) missedVotePenalties(effectiveBalance, totalActiveBalance uint64) (source, target uint64) {
	root := new(big.Int).Sqrt(new(big.Int).SetUint64(totalActiveBalance)).Uint64()
	baseReward := effectiveBalance / spec.increment * (spec.increment * spec.baseRewardFactor / root)
	return baseReward * spec.sourceWeight / spec.weightDenominator,
		baseReward * spec.targetWeight / spec.weightDenominator
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
// validator's effective balance. MissedSource and MissedTarget are what
// missing those votes costs; missing the head vote costs nothing.
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
// target score.
func (validator Validator) VoteAmounts(ctx context.Context, epoch uint64) (VoteAmounts, error) {
	stateID := strconv.FormatUint((epoch+2)*validator.Chain.SlotsPerEpoch-1, 10)
	checkpoints, err := validator.Client.FinalityCheckpointsAt(ctx, stateID)
	if err != nil {
		return VoteAmounts{}, err
	}
	if finalized := checkpoints.Finalized.Epoch; epoch > finalized+validator.Spec.minEpochsToInactivityPenalty {
		return VoteAmounts{}, fmt.Errorf(
			"the rewards of epoch %d are processed in an inactivity leak: the last finalized epoch is %d", epoch, finalized)
	}
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

// BalanceAt is the balance in the state at slot, after its block if it has
// one.
func (validator Validator) BalanceAt(ctx context.Context, slot uint64) (uint64, error) {
	record, err := validator.Client.ValidatorAt(ctx, strconv.FormatUint(slot, 10), strconv.FormatUint(validator.Index, 10))
	return record.Balance, err
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

// TimelyVotes is which of a validator's votes of an epoch the chain counts as
// timely.
type TimelyVotes struct {
	Source bool
	Head   bool
}

// TimelyVotes reads, from the blocks that include the validator's attestation
// of epoch, which of its votes the chain counts as timely. The source vote is
// timely if a block includes it within ⌊√SLOTS_PER_EPOCH⌋ slots; the head vote
// if a block includes it MIN_ATTESTATION_INCLUSION_DELAY slots after the vote,
// for the canonical block at that slot and the epoch's canonical target.
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

// duty finds the validator's attestation duty among committees: the slot, the
// committee index and its position in the committee.
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
