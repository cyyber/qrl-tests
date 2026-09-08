package validatorops

import (
	"encoding/hex"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/consensuscrypto"
	"github.com/stretchr/testify/require"
)

func TestDeterministicKeyIsStable(t *testing.T) {
	first, err := DeterministicKey(0x91)
	require.NoError(t, err)
	second, err := DeterministicKey(0x91)
	require.NoError(t, err)
	other, err := DeterministicKey(0x92)
	require.NoError(t, err)

	require.Len(t, first.PublicKey(), consensuscrypto.PublicKeyLength)
	require.Equal(t, first.PublicKey(), second.PublicKey())
	require.NotEqual(t, first.PublicKey(), other.PublicKey())
}

func TestRandaoCommitmentMatchesValidatorClient(t *testing.T) {
	// Marker 0x91 seeds the onion with 0x91, 0x92, ...; qrysm's crypto/randao
	// produces this commitment for that seed and its default layer count.
	key, err := DeterministicKey(0x91)
	require.NoError(t, err)
	require.Equal(t,
		"3a4c6ce43d45814c5f37ef3d08b4888af27ffbfb8f2870d0229d3fabe941c6b9",
		hex.EncodeToString(key.RandaoCommitment()),
	)
}

func TestSignaturesVerify(t *testing.T) {
	key, err := DeterministicKey(0x91)
	require.NoError(t, err)

	var message [consensuscrypto.RootLength]byte
	message[0] = 1
	signature, err := key.Sign(message[:])
	require.NoError(t, err)
	require.Len(t, signature, consensuscrypto.SignatureLength)
	require.NoError(t, consensuscrypto.Verify(message, key.PublicKey(), signature))

	message[0] = 2
	require.Error(t, consensuscrypto.Verify(message, key.PublicKey(), signature))
}
