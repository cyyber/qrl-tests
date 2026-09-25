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
	return fakeSource{genesis: beacon.Genesis{ForkVersion: "0x" + hex.EncodeToString(genesisVersion[:])}}
}

func TestLoad(t *testing.T) {
	chain, err := Load(t.Context(), validSource())
	require.NoError(t, err)
	require.Equal(t, uint64(128), chain.SlotsPerEpoch)
	require.Equal(t, uint64(2), chain.Epoch(300))
	require.Equal(t, 12*time.Second, chain.SlotDuration)
	require.Equal(t, 4*128*12*time.Second, chain.ExecutionVotingPeriod)
	// The follow distance counts execution blocks, not slots.
	require.Equal(t, 16*14*time.Second, chain.ExecutionFollowDistance)
	require.Equal(t, 16*14*time.Second+3*4*128*12*time.Second/2+4*128*12*time.Second, chain.DepositWait())

	deposit := signing.ComputeDomain(signing.DomainDeposit, genesisVersion, signing.Root{})
	require.Equal(t, deposit, chain.DepositDomain())
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
