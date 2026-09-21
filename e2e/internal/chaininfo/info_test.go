package chaininfo

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/consensuscrypto"
)

type fakeSource struct {
	genesis beacon.Genesis
	fork    beacon.Fork
}

func (source fakeSource) Genesis(context.Context) (beacon.Genesis, error) { return source.genesis, nil }
func (source fakeSource) Fork(context.Context) (beacon.Fork, error)       { return source.fork, nil }
func (fakeSource) SpecUint(context.Context, string) (uint64, error)       { return 128, nil }

func validSource() fakeSource {
	return fakeSource{
		genesis: beacon.Genesis{
			ValidatorsRoot: "0x" + strings.Repeat("55", consensuscrypto.RootLength),
			ForkVersion:    "0x10000020",
		},
		fork: beacon.Fork{PreviousVersion: "0x10000020", CurrentVersion: "0x10000021", Epoch: 6},
	}
}

func TestLoadDerivesDomainsFromTheForkSchedule(t *testing.T) {
	chain, err := Load(t.Context(), validSource())
	require.NoError(t, err)
	require.Equal(t, uint64(128), chain.SlotsPerEpoch)
	require.Equal(t, uint64(2), chain.Epoch(300))

	var genesisRoot [consensuscrypto.RootLength]byte
	copy(genesisRoot[:], bytes.Repeat([]byte{0x55}, consensuscrypto.RootLength))
	previous := consensuscrypto.ComputeDomain(consensuscrypto.DomainVoluntaryExit, [4]byte{0x10, 0x00, 0x00, 0x20}, genesisRoot)
	current := consensuscrypto.ComputeDomain(consensuscrypto.DomainVoluntaryExit, [4]byte{0x10, 0x00, 0x00, 0x21}, genesisRoot)

	require.Equal(t, previous, chain.Domain(consensuscrypto.DomainVoluntaryExit, 5))
	require.Equal(t, current, chain.Domain(consensuscrypto.DomainVoluntaryExit, 6))
	require.Equal(t, current, chain.Domain(consensuscrypto.DomainVoluntaryExit, 7))

	deposit := consensuscrypto.ComputeDomain(
		consensuscrypto.DomainDeposit, [4]byte{0x10, 0x00, 0x00, 0x20}, [consensuscrypto.RootLength]byte{},
	)
	require.Equal(t, deposit, chain.DepositDomain())
}

func TestLoadRejectsMalformedValues(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*fakeSource)
		want   string
	}{
		{"short genesis root", func(s *fakeSource) { s.genesis.ValidatorsRoot = "0x5555" }, "invalid genesis validators root length 2, want 32"},
		{"non-hex fork version", func(s *fakeSource) { s.fork.CurrentVersion = "0xzz000021" }, "decode current fork version"},
		{"long previous version", func(s *fakeSource) { s.fork.PreviousVersion = "0x1000002000" }, "invalid previous fork version length 5, want 4"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := validSource()
			test.mutate(&source)
			_, err := Load(t.Context(), source)
			require.ErrorContains(t, err, test.want)
		})
	}
}
