package chaininfo

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/signing"
)

var (
	genesisValidatorsRoot = [signing.RootLength]byte(bytes.Repeat([]byte{0x55}, signing.RootLength))
	genesisVersion        = signing.ForkVersion{0x10, 0x00, 0x00, 0x20}
	currentVersion        = signing.ForkVersion{0x10, 0x00, 0x00, 0x21}
)

func hex0x(value []byte) string { return "0x" + hex.EncodeToString(value) }

type fakeSource struct {
	genesis beacon.Genesis
	fork    beacon.Fork
}

func (source fakeSource) Genesis(context.Context) (beacon.Genesis, error) { return source.genesis, nil }
func (source fakeSource) Fork(context.Context) (beacon.Fork, error)       { return source.fork, nil }

func (fakeSource) SpecUint(_ context.Context, name string) (uint64, error) {
	if name != "SLOTS_PER_EPOCH" {
		return 0, fmt.Errorf("unexpected spec value %s", name)
	}
	return 128, nil
}

func validSource() fakeSource {
	return fakeSource{
		genesis: beacon.Genesis{
			ValidatorsRoot: hex0x(genesisValidatorsRoot[:]),
			ForkVersion:    hex0x(genesisVersion[:]),
		},
		fork: beacon.Fork{
			PreviousVersion: hex0x(genesisVersion[:]),
			CurrentVersion:  hex0x(currentVersion[:]),
			Epoch:           6,
		},
	}
}

func TestLoadDerivesDomainsFromTheForkSchedule(t *testing.T) {
	chain, err := Load(t.Context(), validSource())
	require.NoError(t, err)
	require.Equal(t, uint64(128), chain.SlotsPerEpoch)
	require.Equal(t, uint64(2), chain.Epoch(300))

	previous := signing.ComputeDomain(signing.DomainVoluntaryExit, genesisVersion, genesisValidatorsRoot)
	current := signing.ComputeDomain(signing.DomainVoluntaryExit, currentVersion, genesisValidatorsRoot)
	require.Equal(t, previous, chain.Domain(signing.DomainVoluntaryExit, 5))
	require.Equal(t, current, chain.Domain(signing.DomainVoluntaryExit, 6))
	require.Equal(t, current, chain.Domain(signing.DomainVoluntaryExit, 7))

	deposit := signing.ComputeDomain(signing.DomainDeposit, genesisVersion, [signing.RootLength]byte{})
	require.Equal(t, deposit, chain.DepositDomain())
}

func TestLoadRejectsMalformedValues(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*fakeSource)
		want   string
	}{
		{
			"short genesis root",
			func(source *fakeSource) { source.genesis.ValidatorsRoot = "0x5555" },
			"invalid genesis validators root length 2, want 32",
		},
		{
			"non-hex fork version",
			func(source *fakeSource) { source.fork.CurrentVersion = "0xzz000021" },
			"decode current fork version",
		},
		{
			"long previous version",
			func(source *fakeSource) { source.fork.PreviousVersion = "0x1000002000" },
			"invalid previous fork version length 5, want 4",
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
