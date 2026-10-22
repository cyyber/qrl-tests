package networkrestart

import (
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/stretchr/testify/require"
)

func TestSelectKillPhases(t *testing.T) {
	keys := func(phases []killPhase) []string { return phaseKeys(phases) }
	all := []string{"slot-start", "slot-third", "slot-two-thirds", "epoch-end"}

	t.Run("unset keeps every phase", func(t *testing.T) {
		selected, err := selectKillPhases("", killPhases)
		require.NoError(t, err)
		require.Equal(t, all, keys(selected))

		selected, err = selectKillPhases("  ", killPhases)
		require.NoError(t, err)
		require.Equal(t, all, keys(selected))
	})

	t.Run("by key, in the phases' order", func(t *testing.T) {
		selected, err := selectKillPhases("epoch-end, slot-third", killPhases)
		require.NoError(t, err)
		require.Equal(t, []string{"slot-third", "epoch-end"}, keys(selected))
	})

	t.Run("by position", func(t *testing.T) {
		selected, err := selectKillPhases("3,1", killPhases)
		require.NoError(t, err)
		require.Equal(t, []string{"slot-start", "slot-two-thirds"}, keys(selected))
	})

	t.Run("mixed and repeated", func(t *testing.T) {
		selected, err := selectKillPhases("slot-third,2,slot-third", killPhases)
		require.NoError(t, err)
		require.Equal(t, []string{"slot-third"}, keys(selected))
	})

	t.Run("unknown key", func(t *testing.T) {
		_, err := selectKillPhases("slot-start,slot-end", killPhases)
		require.EqualError(t, err,
			`RESILIENCE_KILL_PHASES: unknown phase "slot-end", want one of slot-start, slot-third, slot-two-thirds, epoch-end`)
	})

	t.Run("position outside the list", func(t *testing.T) {
		_, err := selectKillPhases("5", killPhases)
		require.EqualError(t, err, "RESILIENCE_KILL_PHASES: phase 5 is outside 1 to 4")
		_, err = selectKillPhases("0", killPhases)
		require.EqualError(t, err, "RESILIENCE_KILL_PHASES: phase 0 is outside 1 to 4")
	})

	t.Run("env", func(t *testing.T) {
		t.Setenv(KillPhasesEnv, "epoch-end")
		selected, err := selectedKillPhases()
		require.NoError(t, err)
		require.Equal(t, []string{"epoch-end"}, keys(selected))
	})
}

func TestKillPhaseTiming(t *testing.T) {
	chain := chaininfo.Info{SlotsPerEpoch: 8, SlotDuration: 5 * time.Second}
	offsets := make([]time.Duration, len(killPhases))
	for index, phase := range killPhases {
		offsets[index] = phase.offset(chain)
	}
	require.Equal(t, []time.Duration{0, 5 * time.Second / 3, 10 * time.Second / 3, 4500 * time.Millisecond}, offsets)

	// Only the last phase waits for the last slot of an epoch.
	for _, phase := range killPhases[:3] {
		require.True(t, phase.slots(chain)(6), phase.key)
	}
	require.False(t, killPhases[3].slots(chain)(6))
	require.True(t, killPhases[3].slots(chain)(7))
}
