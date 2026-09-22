package validatorops

import (
	"context"
	"fmt"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/signing"
	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/common/hexutil"
)

var signingGenesisRoot = signing.Root{0x55}

type signingSource struct{}

func (signingSource) Genesis(context.Context) (beacon.Genesis, error) {
	return beacon.Genesis{
		ValidatorsRoot: hexutil.Encode(signingGenesisRoot[:]),
		ForkVersion:    "0x10000020",
	}, nil
}

func (signingSource) Fork(context.Context) (beacon.Fork, error) {
	return beacon.Fork{PreviousVersion: "0x10000021", CurrentVersion: "0x10000022", Epoch: 12}, nil
}

func (signingSource) SpecUint(_ context.Context, name string) (uint64, error) {
	if name != "SLOTS_PER_EPOCH" {
		return 0, fmt.Errorf("unexpected spec value %s", name)
	}
	return 8, nil
}

func loadChainInfo(t *testing.T) chaininfo.Info {
	t.Helper()
	chain, err := chaininfo.Load(t.Context(), signingSource{})
	require.NoError(t, err)
	return chain
}

func TestVoluntaryExitSignsRequestedData(t *testing.T) {
	chain := loadChainInfo(t)
	key, err := DeterministicKey(0x91)
	require.NoError(t, err)
	for _, test := range []struct {
		epoch   uint64
		version signing.ForkVersion
	}{
		{11, signing.ForkVersion{0x10, 0, 0, 0x21}},
		{12, signing.ForkVersion{0x10, 0, 0, 0x22}},
	} {
		exit, err := VoluntaryExit(key, 64, test.epoch, chain)
		require.NoError(t, err)
		require.Equal(t, beacon.VoluntaryExit{Epoch: test.epoch, ValidatorIndex: 64}, exit.Message)
		signature, err := hexutil.Decode(exit.Signature)
		require.NoError(t, err)
		root, err := (signing.VoluntaryExit{Epoch: test.epoch, ValidatorIndex: 64}).HashTreeRoot()
		require.NoError(t, err)
		domain := signing.ComputeDomain(signing.DomainVoluntaryExit, test.version, signingGenesisRoot)
		require.NoError(t, signing.Verify(signing.SigningRoot(root, domain), key.PublicKey(), signature), "epoch %d", test.epoch)
	}
}
