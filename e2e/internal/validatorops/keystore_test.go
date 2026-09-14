package validatorops

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeystoreRoundTrip(t *testing.T) {
	key, err := DeterministicKey(0x91)
	require.NoError(t, err)

	encoded, err := key.KeystoreJSON(KeystorePassword)
	require.NoError(t, err)

	var file keystoreFile
	require.NoError(t, json.Unmarshal([]byte(encoded), &file))
	require.Equal(t, uint(1), file.Version)
	require.NotEmpty(t, file.UUID)
	require.Equal(t, hex.EncodeToString(key.PublicKey()), file.Pubkey)

	secret, err := decryptSecret(file.Crypto, KeystorePassword)
	require.NoError(t, err)
	require.Equal(t, key.Seed(), secret)
}
