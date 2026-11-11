package rewards

import "slices"

// Stake is an offline validator's balance, effective balance and inactivity
// score at the start of an epoch.
type Stake struct {
	Balance          uint64
	EffectiveBalance uint64
	InactivityScore  uint64
}

// Outage is the chain at the start of Epoch, from the epoch before which the
// offline validators miss every vote. Online is the effective balance of the
// other validators, which the forecast holds fixed: they vote every timely
// target, so the leak takes nothing from them.
type Outage struct {
	Epoch     uint64
	Finalized uint64
	Online    uint64
	Offline   []Stake
}

// Recovery is the first epoch the online validators justify alone, and what
// the inactivity leak leaves the offline ones until then.
type Recovery struct {
	// Justified is the first epoch whose votes count at least two thirds of
	// the stake once the epoch after it ends: the one before the first epoch
	// that starts with the online stake at least twice the offline stake.
	Justified uint64
	start     uint64
	stakes    [][]Stake
}

// At returns the offline validators' stakes at the start of epoch, for the
// epochs from the outage's through Justified+2.
func (recovery Recovery) At(epoch uint64) ([]Stake, bool) {
	if epoch < recovery.start || epoch-recovery.start >= uint64(len(recovery.stakes)) {
		return nil, false
	}
	return slices.Clone(recovery.stakes[epoch-recovery.start]), true
}

// ForecastRecovery simulates the outage epoch by epoch until the online
// validators hold two thirds of the effective balance.
func (spec Spec) ForecastRecovery(outage Outage) Recovery {
	recovery := Recovery{start: outage.Epoch}
	stakes := slices.Clone(outage.Offline)
	justified := false
	for epoch := outage.Epoch; ; epoch++ {
		recovery.stakes = append(recovery.stakes, slices.Clone(stakes))
		var offline uint64
		for _, stake := range stakes {
			offline += stake.EffectiveBalance
		}
		total := outage.Online + offline
		// The processing at the end of epoch counts the votes of the one
		// before, at the effective balances it starts with.
		if !justified && 3*outage.Online >= 2*total {
			recovery.Justified, justified = epoch-1, true
		}
		if justified && epoch == recovery.Justified+2 {
			return recovery
		}
		leak := spec.inInactivityLeak(epoch-1, outage.Finalized)
		for index := range stakes {
			stakes[index] = spec.nextStake(stakes[index], outage.Online, total, leak)
		}
	}
}

// nextStake is an offline validator's stake one epoch on: after the epoch's
// blocks, and the processing at its end, which penalizes the missed votes of
// the epoch before and updates the effective balance.
func (spec Spec) nextStake(stake Stake, online, total uint64, leak bool) Stake {
	baseReward := spec.baseReward(stake.EffectiveBalance, total)
	// Sync committee seats and proposers are both drawn by effective balance,
	// so the seats a validator misses cost it, on average, this share of its
	// base reward per epoch of blocks, scaled to the online proposers' part.
	sync := baseReward * spec.syncRewardWeight / spec.weightDenominator *
		(online / spec.increment) / (total / spec.increment)
	source, target := spec.missedVotePenalties(stake.EffectiveBalance, total)
	stake.InactivityScore = spec.NextInactivityScore(stake.InactivityScore, false, leak)
	penalty := sync + source + target + spec.inactivityPenalty(stake.EffectiveBalance, stake.InactivityScore)
	stake.Balance -= min(stake.Balance, penalty)
	// The effective balance drops to the balance's whole increments once the
	// balance falls a hysteresis step below it.
	if stake.Balance+spec.increment/spec.hysteresisQuotient*spec.hysteresisDownwardMultiplier < stake.EffectiveBalance {
		stake.EffectiveBalance = stake.Balance - stake.Balance%spec.increment
	}
	return stake
}
