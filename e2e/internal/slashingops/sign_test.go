package slashingops

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/signing"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
	"github.com/stretchr/testify/require"
)

func hexRoot(value byte) string {
	return "0x" + strings.Repeat(hex.EncodeToString([]byte{value}), signing.RootLength)
}

func TestSignProposerSlashing(t *testing.T) {
	key, err := validatorops.DeterministicKey(0x91)
	require.NoError(t, err)
	signer := validatorops.Signer{
		Key:                   key,
		Fork:                  signing.Fork{CurrentVersion: signing.ForkVersion{0x10, 0x00, 0x00, 0x20}},
		GenesisValidatorsRoot: signing.Root{0x77},
	}
	first := beacon.BeaconBlockHeaderMessage{
		Slot: 123, ProposerIndex: 45, ParentRoot: hexRoot(0x11), StateRoot: hexRoot(0x22), BodyRoot: hexRoot(0x33),
	}
	second := first
	second.BodyRoot = hexRoot(0x44)

	slashing, err := SignProposerSlashing(signer, 8, first, second)
	require.NoError(t, err)
	require.Equal(t, first, slashing.SignedHeader1.Message)
	require.Equal(t, second, slashing.SignedHeader2.Message)

	// Qrysm computes this root for the first header.
	root, err := headerRoot(first)
	require.NoError(t, err)
	require.Equal(t, "43d88b69660aef4a5898e981591de4f5114841ab7e04c312212bbb8d0fe0c85a", hex.EncodeToString(root[:]))
	domain := signer.Fork.Domain(signing.DomainBeaconProposer, 15, signer.GenesisValidatorsRoot)
	signature, err := hex.DecodeString(strings.TrimPrefix(slashing.SignedHeader1.Signature, "0x"))
	require.NoError(t, err)
	require.NoError(t, signing.Verify(signing.SigningRoot(root, domain), key.PublicKey(), signature))

	second.ParentRoot = "0x01"
	_, err = SignProposerSlashing(signer, 8, first, second)
	require.EqualError(t, err, `header of slot 123: root "0x01" is 1 bytes, want 32`)
}
