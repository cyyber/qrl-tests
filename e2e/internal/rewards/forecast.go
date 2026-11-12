package rewards

import (
	"errors"
	"slices"
)

// Stake is an offline validator's balances and inactivity score at the start
// of an epoch.
type Stake struct {
	Balance          uint64
	EffectiveBalance uint64
	InactivityScore  uint64
}

// Outage is the chain at the start of Epoch, with the offline validators
// missing every vote since the epoch before. Online, the other validators'
// effective balance, stays fixed: they vote on time, so the leak takes nothing.
type Outage struct {
	Epoch     uint64
	Finalized uint64
	Online    uint64
	Offline   []Stake
}

// Recovery is the first epoch the online validators justify alone, and the
// offline stakes until then.
type Recovery struct {
	// Justified is the first epoch whose votes reach two thirds of the stake.
	Justified uint64
	start     uint64
	stakes    [][]Stake
}

// At returns the offline stakes at the start of epoch, from the outage's epoch
// through Justified+2.
func (recovery Recovery) At(epoch uint64) ([]Stake, bool) {
	if epoch < recovery.start || epoch-recovery.start >= uint64(len(recovery.stakes)) {
		return nil, false
	}
	return slices.Clone(recovery.stakes[epoch-recovery.start]), true
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
func (spec Spec) ForecastRecovery(outage Outage) (Recovery, error) {
	// With no online stake, two thirds is never reached.
	if outage.Online == 0 {
		return Recovery{}, errors.New("forecast a recovery: the online validators hold no stake")
	}
	recovery := Recovery{start: outage.Epoch}
	stakes := slices.Clone(outage.Offline)
	justified := false
	for epoch := outage.Epoch; ; epoch++ {
		recovery.stakes = append(recovery.stakes, slices.Clone(stakes))
		total := outage.Online + TotalEffectiveBalance(stakes)
		// The processing at the end of epoch counts the previous epoch's votes
		// at these effective balances.
		if !justified && 3*outage.Online >= 2*total {
			recovery.Justified, justified = epoch-1, true
		}
		if justified && epoch == recovery.Justified+2 {
			return recovery, nil
		}
		leak := spec.inInactivityLeak(epoch-1, outage.Finalized)
		for index := range stakes {
			stakes[index] = spec.nextStake(stakes[index], outage.Online, total, leak)
		}
	}
}

// nextStake is an offline validator's stake an epoch later.
func (spec Spec) nextStake(stake Stake, online, total uint64, leak bool) Stake {
	baseReward := spec.baseReward(stake.EffectiveBalance, total)
	// The average cost of missed sync committee duties in the online
	// validators' blocks.
	syncPenalty := baseReward * spec.syncRewardWeight / spec.weightDenominator *
		(online / spec.increment) / (total / spec.increment)
	source, target := spec.missedVotePenalties(stake.EffectiveBalance, total)
	stake.InactivityScore = spec.NextInactivityScore(stake.InactivityScore, false, leak)
	penalty := syncPenalty + source + target + spec.inactivityPenalty(stake.EffectiveBalance, stake.InactivityScore)
	stake.Balance -= min(stake.Balance, penalty)
	// The effective balance drops only once the balance falls a hysteresis step
	// below it.
	if stake.Balance+spec.increment/spec.hysteresisQuotient*spec.hysteresisDownwardMultiplier < stake.EffectiveBalance {
		stake.EffectiveBalance = stake.Balance - stake.Balance%spec.increment
	}
	return stake
}
