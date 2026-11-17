package validatorops

import (
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/signing"
	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/common/hexutil"
)

func TestSignVoluntaryExit(t *testing.T) {
	key, err := DeterministicKey(0x43)
	require.NoError(t, err)
	fork, root := signing.ForkVersion{0x10, 0, 0, 0x38}, signing.Root{1, 2, 3}

	exit, err := key.SignVoluntaryExit(7, 12, fork, root)
	require.NoError(t, err)
	require.Equal(t, uint64(7), exit.Message.ValidatorIndex)
	require.Equal(t, uint64(12), exit.Message.Epoch)

	signature, err := hexutil.Decode(exit.Signature)
	require.NoError(t, err)
	domain := signing.ComputeDomain(signing.DomainVoluntaryExit, fork, root)
	signingRoot := signing.SigningRoot(signing.VoluntaryExit{Epoch: 12, ValidatorIndex: 7}.HashTreeRoot(), domain)
	require.NoError(t, signing.Verify(signingRoot, key.PublicKey(), signature))

	// The exit of another validator, or another epoch, has another root.
	other := signing.SigningRoot(signing.VoluntaryExit{Epoch: 12, ValidatorIndex: 8}.HashTreeRoot(), domain)
	require.Error(t, signing.Verify(other, key.PublicKey(), signature))
}

func TestSignDoubleProposal(t *testing.T) {
	key, err := DeterministicKey(0x44)
	require.NoError(t, err)
	fork, root := signing.ForkVersion{0x10, 0, 0, 0x38}, signing.Root{1, 2, 3}

	slashing, err := key.SignDoubleProposal(9, 30, fork, root)
	require.NoError(t, err)
	first, second := slashing.SignedHeader1, slashing.SignedHeader2
	require.Equal(t, first.Message.Slot, second.Message.Slot)
	require.Equal(t, uint64(9), first.Message.ProposerIndex)
	require.NotEqual(t, first.Message.BodyRoot, second.Message.BodyRoot)

	domain := signing.ComputeDomain(signing.DomainBeaconProposer, fork, root)
	for index, signed := range []struct {
		body      signing.Root
		signature string
	}{{signing.Root{1}, first.Signature}, {signing.Root{2}, second.Signature}} {
		signature, err := hexutil.Decode(signed.signature)
		require.NoError(t, err)
		header := signing.BeaconBlockHeader{Slot: 30, ProposerIndex: 9, BodyRoot: signed.body}
		require.NoError(t, signing.Verify(signing.SigningRoot(header.HashTreeRoot(), domain), key.PublicKey(), signature), "header %d", index+1)
	}
}
