package chaininfo

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/signing"
)

var genesisVersion = signing.ForkVersion{0x10, 0x00, 0x00, 0x20}

type fakeSource struct {
	genesis beacon.Genesis
}

func (source fakeSource) Genesis(context.Context) (beacon.Genesis, error) { return source.genesis, nil }

func (fakeSource) SpecUint(_ context.Context, name string) (uint64, error) {
	switch name {
	case "SLOTS_PER_EPOCH":
		return 128, nil
	case "SECONDS_PER_SLOT":
		return 12, nil
	case "EPOCHS_PER_EXECUTION_VOTING_PERIOD":
		return 4, nil
	case "EXECUTION_FOLLOW_DISTANCE":
		return 16, nil
	case "SECONDS_PER_EXECUTION_BLOCK":
		return 14, nil
	default:
		return 0, fmt.Errorf("unexpected spec value %s", name)
	}
}

func validSource() fakeSource {
	return fakeSource{genesis: beacon.Genesis{
		Time:        1_700_000_000,
		ForkVersion: "0x" + hex.EncodeToString(genesisVersion[:]),
	}}
}

func TestLoad(t *testing.T) {
	chain, err := Load(t.Context(), validSource())
	require.NoError(t, err)
	require.Equal(t, time.Unix(1_700_000_000, 0), chain.GenesisTime)
	require.Equal(t, uint64(128), chain.SlotsPerEpoch)
	require.Equal(t, uint64(2), chain.Epoch(300))
	require.Equal(t, 12*time.Second, chain.SlotDuration)
	require.Equal(t, 128*12*time.Second, chain.EpochDuration)
	require.Equal(t, 4*128*12*time.Second, chain.ExecutionVotingPeriod)
	// The follow distance counts execution blocks, not slots.
	require.Equal(t, 16*14*time.Second, chain.ExecutionFollowDistance)
	require.Equal(t, 16*14*time.Second+3*4*128*12*time.Second/2+4*128*12*time.Second, chain.DepositWait())

	deposit := signing.ComputeDomain(signing.DomainDeposit, genesisVersion, signing.Root{})
	require.Equal(t, deposit, chain.DepositDomain())
	require.Equal(t, "0x10000020", chain.GenesisForkVersion())
}

func TestLoadRejectsMalformedValues(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*fakeSource)
		want   string
	}{
		{
			"non-hex fork version",
			func(source *fakeSource) { source.genesis.ForkVersion = "0xzz000020" },
			"decode genesis fork version",
		},
		{
			"long fork version",
			func(source *fakeSource) { source.genesis.ForkVersion = "0x1000002000" },
			"invalid genesis fork version length 5, want 4",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := validSource()
			test.mutate(&source)
			_, err := Load(t.Context(), source)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestSlotClock(t *testing.T) {
	chain, err := Load(t.Context(), validSource())
	require.NoError(t, err)
	genesis := chain.GenesisTime

	require.Equal(t, SlotPoint{Slot: 0, Offset: 0}, chain.SlotPointAt(genesis))
	require.Equal(t, SlotPoint{Slot: 0, Offset: -time.Second}, chain.SlotPointAt(genesis.Add(-time.Second)))
	require.Equal(t, SlotPoint{Slot: 2, Offset: 3 * time.Second}, chain.SlotPointAt(genesis.Add(27*time.Second)))
	require.Equal(t, SlotPoint{Slot: 3, Offset: 0}, chain.SlotPointAt(genesis.Add(36*time.Second)))
	require.Equal(t, "slot 2 at 3.000s", SlotPoint{Slot: 2, Offset: 3 * time.Second}.String())

	t.Run("later in the same slot", func(t *testing.T) {
		next, err := chain.NextSlotOffset(genesis.Add(27*time.Second), 4*time.Second, AnySlot)
		require.NoError(t, err)
		require.Equal(t, genesis.Add(28*time.Second), next)
	})

	t.Run("already at the offset", func(t *testing.T) {
		next, err := chain.NextSlotOffset(genesis.Add(27*time.Second), 3*time.Second, AnySlot)
		require.NoError(t, err)
		require.Equal(t, genesis.Add(27*time.Second), next)
	})

	t.Run("past the offset, so in the next slot", func(t *testing.T) {
		next, err := chain.NextSlotOffset(genesis.Add(27*time.Second), 2*time.Second, AnySlot)
		require.NoError(t, err)
		require.Equal(t, genesis.Add(38*time.Second), next)
	})

	t.Run("before genesis", func(t *testing.T) {
		next, err := chain.NextSlotOffset(genesis.Add(-5*time.Second), 2*time.Second, AnySlot)
		require.NoError(t, err)
		require.Equal(t, genesis.Add(2*time.Second), next)
	})

	t.Run("last slot of the epoch", func(t *testing.T) {
		// 128 slots of 12 s per epoch: the last slot of epoch 0 starts at
		// 127 * 12 s = 1524 s.
		require.False(t, chain.LastSlotOfEpoch(126))
		require.True(t, chain.LastSlotOfEpoch(127))
		require.True(t, chain.LastSlotOfEpoch(255))

		next, err := chain.NextSlotOffset(genesis.Add(27*time.Second), 10*time.Second, chain.LastSlotOfEpoch)
		require.NoError(t, err)
		require.Equal(t, genesis.Add(1534*time.Second), next)

		// Already past the offset in the last slot: the next epoch's.
		next, err = chain.NextSlotOffset(genesis.Add(1535*time.Second), 10*time.Second, chain.LastSlotOfEpoch)
		require.NoError(t, err)
		require.Equal(t, genesis.Add((1534+128*12)*time.Second), next)
	})

	t.Run("filter accepts nothing", func(t *testing.T) {
		_, err := chain.NextSlotOffset(genesis.Add(27*time.Second), time.Second, func(uint64) bool { return false })
		require.EqualError(t, err, "no slot accepted in the epoch after slot 2 at 3.000s")
	})

	t.Run("offset outside the slot", func(t *testing.T) {
		_, err := chain.NextSlotOffset(genesis, 12*time.Second, AnySlot)
		require.EqualError(t, err, "slot offset 12s is outside a 12s slot")
		_, err = chain.NextSlotOffset(genesis, -time.Second, AnySlot)
		require.EqualError(t, err, "slot offset -1s is outside a 12s slot")
	})
}

func TestWaitForSlotOffset(t *testing.T) {
	chain, err := Load(t.Context(), validSource())
	require.NoError(t, err)
	// Genesis just now, so the wait is short. The timer may wake up a
	// little late, but not 50 ms late.
	chain.GenesisTime = time.Now()
	chain.SlotDuration = 200 * time.Millisecond

	point, err := chain.WaitForSlotOffset(t.Context(), 100*time.Millisecond, AnySlot)
	require.NoError(t, err)
	require.GreaterOrEqual(t, point.Offset, 100*time.Millisecond)
	require.Less(t, point.Offset, 150*time.Millisecond)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = chain.WaitForSlotOffset(ctx, 100*time.Millisecond, AnySlot)
	require.ErrorIs(t, err, context.Canceled)

	_, err = chain.WaitForSlotOffset(t.Context(), time.Second, AnySlot)
	require.EqualError(t, err, "slot offset 1s is outside a 200ms slot")
}
