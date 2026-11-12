package rewards

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

const fullStake = 40_000 * shorPerQRL

func TestNextStake(t *testing.T) {
	spec := testSpec()
	for name, test := range map[string]struct {
		stake Stake
		leak  bool
		want  Stake
	}{
		// 43 of 65 validators at 40,000 QRL online: the base reward is
		// 1,606,560,000 shor, so the missed votes cost 351,435,000 and
		// 652,665,000 shor, and the sync committee seats 33,212,538 on
		// average. The score stays zero, and the effective balance drops two
		// increments.
		"outside a leak": {
			stake: Stake{Balance: fullStake, EffectiveBalance: fullStake},
			want:  Stake{Balance: 39_998_962_687_462, EffectiveBalance: 39_998 * shorPerQRL},
		},
		// The score rises to 12, which adds 1,830,596,923 shor at 39,990 QRL.
		"in a leak": {
			stake: Stake{Balance: 39_990_600_000_000, EffectiveBalance: 39_990 * shorPerQRL, InactivityScore: 8},
			leak:  true,
			want:  Stake{Balance: 39_987_732_349_869, EffectiveBalance: 39_987 * shorPerQRL, InactivityScore: 12},
		},
		// A balance less than a quarter increment below the effective balance
		// leaves it.
		"within the hysteresis": {
			stake: Stake{Balance: 39_990_837_053_208, EffectiveBalance: 39_990 * shorPerQRL},
			want:  Stake{Balance: 39_989_800_000_000, EffectiveBalance: 39_990 * shorPerQRL},
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.want, spec.nextStake(test.stake, 43*fullStake, 65*fullStake, test.leak))
		})
	}
}

func TestMissedSyncPenalty(t *testing.T) {
	// The 1,606,560,000 shor base reward of 40,000 QRL among 65 validators,
	// times 2/64, scaled to the 43 online of 65.
	require.Equal(t, uint64(33_212_538), testSpec().missedSyncPenalty(fullStake, 43*fullStake, 65*fullStake))
}

func TestNextEffectiveBalance(t *testing.T) {
	spec := testSpec()
	effective := uint64(39_990 * shorPerQRL)
	for name, test := range map[string]struct {
		balance, want uint64
	}{
		"a quarter increment below keeps it":     {balance: effective - shorPerQRL/4, want: effective},
		"just past a quarter increment drops it": {balance: effective - shorPerQRL/4 - 1, want: 39_989 * shorPerQRL},
		"a larger fall drops several increments": {balance: 39_987_500_000_000, want: 39_987 * shorPerQRL},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.want, spec.nextEffectiveBalance(test.balance, effective))
		})
	}
}

func TestForecastRecoveryAfterOneEpoch(t *testing.T) {
	spec := testSpec()
	// One validator offline against 79,998 QRL holds just over a third. Its
	// first missed epoch takes it to 39,995 QRL, under half the online stake,
	// so the online votes of epoch 10 count two thirds at the end of epoch 11.
	start := Stake{Balance: fullStake, EffectiveBalance: fullStake}
	forecast, err := spec.ForecastRecovery(Outage{
		Epoch: 10, FinalizedEpoch: 8, OnlineStake: 79_998 * shorPerQRL, OfflineStakes: []Stake{start},
	})
	require.NoError(t, err)
	require.Equal(t, uint64(10), forecast.FirstJustified)

	stakes, ok := forecast.OfflineStakesAt(10)
	require.True(t, ok)
	require.Equal(t, []Stake{start}, stakes)
	stakes, ok = forecast.OfflineStakesAt(11)
	require.True(t, ok)
	require.Equal(t, []Stake{{Balance: 39_995_170_278_799, EffectiveBalance: 39_995 * shorPerQRL}}, stakes)
	_, ok = forecast.OfflineStakesAt(12)
	require.True(t, ok)
	_, ok = forecast.OfflineStakesAt(13)
	require.False(t, ok)
	_, ok = forecast.OfflineStakesAt(9)
	require.False(t, ok)
}

func TestForecastRecovery(t *testing.T) {
	spec := testSpec()
	// 22 of 65 validators offline from epoch 6 on, with epoch 4 finalized, so
	// the leak counts their missed votes from epoch 9.
	offline := slices.Repeat([]Stake{{Balance: fullStake, EffectiveBalance: fullStake}}, 22)
	forecast, err := spec.ForecastRecovery(Outage{
		Epoch: 7, FinalizedEpoch: 4, OnlineStake: 43 * fullStake, OfflineStakes: offline,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(62), forecast.FirstJustified)

	// Epoch 62 starts with the offline stake at 860,442 QRL, above half the
	// online 1,720,000, and epoch 63, after 53 epochs of leak, at 859,716.
	stakes, ok := forecast.OfflineStakesAt(62)
	require.True(t, ok)
	require.Equal(t, slices.Repeat([]Stake{{
		Balance: 39_111_585_777_860, EffectiveBalance: 39_111 * shorPerQRL, InactivityScore: 208,
	}}, 22), stakes)
	stakes, ok = forecast.OfflineStakesAt(63)
	require.True(t, ok)
	require.Equal(t, slices.Repeat([]Stake{{
		Balance: 39_078_937_749_638, EffectiveBalance: 39_078 * shorPerQRL, InactivityScore: 212,
	}}, 22), stakes)
	_, ok = forecast.OfflineStakesAt(64)
	require.True(t, ok)
	_, ok = forecast.OfflineStakesAt(65)
	require.False(t, ok)
}

func TestForecastRecoveryRefuses(t *testing.T) {
	full := []Stake{{Balance: fullStake, EffectiveBalance: fullStake}}
	// An effective balance one increment over the ejection balance falls to it
	// after one epoch, long before a tiny online stake holds two thirds.
	nearEjection := []Stake{{Balance: 20_000_500_000_000, EffectiveBalance: 20_001 * shorPerQRL}}
	for name, test := range map[string]struct {
		outage Outage
		want   string
	}{
		"no online stake": {
			outage: Outage{Epoch: 7, FinalizedEpoch: 4, OfflineStakes: full},
			want:   "forecast a recovery: the online validators hold no stake",
		},
		"an outage at epoch 0": {
			outage: Outage{OnlineStake: fullStake, OfflineStakes: full},
			want:   "forecast a recovery: the outage starts at epoch 0",
		},
		"an ejection before the recovery": {
			outage: Outage{Epoch: 7, FinalizedEpoch: 4, OnlineStake: shorPerQRL, OfflineStakes: nearEjection},
			want:   "forecast a recovery: offline validator 0 reaches the ejection balance in epoch 8",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := testSpec().ForecastRecovery(test.outage)
			require.EqualError(t, err, test.want)
		})
	}
}

func TestTotalEffectiveBalance(t *testing.T) {
	require.Zero(t, TotalEffectiveBalance(nil))
	require.Equal(t, uint64(79_995*shorPerQRL), TotalEffectiveBalance([]Stake{
		{Balance: fullStake, EffectiveBalance: fullStake},
		{Balance: 39_995_170_278_799, EffectiveBalance: 39_995 * shorPerQRL},
	}))
}
