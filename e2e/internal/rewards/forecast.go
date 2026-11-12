package rewards

import (
	"errors"
	"fmt"
	"slices"
)

// Stake is an offline validator's balances and inactivity score at the start
// of an epoch.
type Stake struct {
	Balance          uint64
	EffectiveBalance uint64
	InactivityScore  uint64
}

// Outage is the chain at the start of Epoch, while the offline validators miss
// every vote.
type Outage struct {
	Epoch          uint64
	FinalizedEpoch uint64
	OnlineStake    uint64
	OfflineStakes  []Stake
}

// RecoveryForecast is when the online validators are expected to first justify
// alone.
type RecoveryForecast struct {
	FirstJustified uint64
	start          uint64
	stakes         [][]Stake
}

// OfflineStakesAt returns the offline stakes at the start of epoch, from the
// outage's epoch through FirstJustified+2.
func (forecast RecoveryForecast) OfflineStakesAt(epoch uint64) ([]Stake, bool) {
	if epoch < forecast.start || epoch-forecast.start >= uint64(len(forecast.stakes)) {
		return nil, false
	}
	return slices.Clone(forecast.stakes[epoch-forecast.start]), true
}

// TotalEffectiveBalance sums the stakes' effective balances.
func TotalEffectiveBalance(stakes []Stake) uint64 {
	var total uint64
	for _, stake := range stakes {
		total += stake.EffectiveBalance
	}
	return total
}

// ForecastRecovery simulates the outage epoch by epoch until the online
// validators hold two thirds of the stake.
func (spec Spec) ForecastRecovery(outage Outage) (RecoveryForecast, error) {
	if outage.OnlineStake == 0 {
		return RecoveryForecast{}, errors.New("forecast a recovery: the online validators hold no stake")
	}
	if outage.Epoch == 0 {
		return RecoveryForecast{}, errors.New("forecast a recovery: the outage starts at epoch 0")
	}
	forecast := RecoveryForecast{start: outage.Epoch}
	stakes := slices.Clone(outage.OfflineStakes)
	justified := false
	for epoch := outage.Epoch; ; epoch++ {
		forecast.stakes = append(forecast.stakes, slices.Clone(stakes))
		total := outage.OnlineStake + TotalEffectiveBalance(stakes)
		// This epoch's processing justifies the one before.
		if !justified && 3*outage.OnlineStake >= 2*total {
			forecast.FirstJustified, justified = epoch-1, true
		}
		if justified && epoch == forecast.FirstJustified+2 {
			return forecast, nil
		}
		leak := spec.inInactivityLeak(epoch-1, outage.FinalizedEpoch)
		for index := range stakes {
			stakes[index] = spec.nextStake(stakes[index], outage.OnlineStake, total, leak)
			if stakes[index].EffectiveBalance <= spec.ejectionBalance {
				return RecoveryForecast{}, fmt.Errorf("forecast a recovery: offline validator %d reaches the ejection balance in epoch %d",
					index, epoch+1)
			}
		}
	}
}

// nextStake applies one epoch's penalties to an offline validator's stake.
func (spec Spec) nextStake(stake Stake, online, total uint64, leak bool) Stake {
	stake.InactivityScore = spec.NextInactivityScore(stake.InactivityScore, false, leak)
	source, target := spec.missedVotePenalties(stake.EffectiveBalance, total)
	penalty := source + target +
		spec.missedSyncPenalty(stake.EffectiveBalance, online, total) +
		spec.inactivityPenalty(stake.EffectiveBalance, stake.InactivityScore)
	stake.Balance -= min(stake.Balance, penalty)
	stake.EffectiveBalance = spec.nextEffectiveBalance(stake.Balance, stake.EffectiveBalance)
	return stake
}

// missedSyncPenalty is the average epoch's cost of missed sync committee
// duties, in the online validators' blocks.
func (spec Spec) missedSyncPenalty(effectiveBalance, online, total uint64) uint64 {
	baseReward := spec.baseReward(effectiveBalance, total)
	return baseReward * spec.syncRewardWeight / spec.weightDenominator *
		(online / spec.increment) / (total / spec.increment)
}

// nextEffectiveBalance is the effective balance after the balance changes: it
// drops to the balance rounded down to whole increments, but only once the
// balance is more than a hysteresis step (a quarter QRL) below it.
func (spec Spec) nextEffectiveBalance(balance, effectiveBalance uint64) uint64 {
	step := spec.increment / spec.hysteresisQuotient * spec.hysteresisDownwardMultiplier
	if balance+step < effectiveBalance {
		return balance - balance%spec.increment
	}
	return effectiveBalance
}
