package validatorops

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeystoreJSON(t *testing.T) {
	key, err := DeterministicKey(0x91)
	require.NoError(t, err)

	encoded, err := key.KeystoreJSON(KeystorePassword)
	require.NoError(t, err)

	var file keystoreFile
	require.NoError(t, json.Unmarshal([]byte(encoded), &file))
	require.Equal(t, uint(1), file.Version)
	require.NotEmpty(t, file.UUID)
	require.Equal(t, hex.EncodeToString(key.PublicKey()), file.Pubkey)
	require.Equal(t, "argon2id", file.Crypto.KDF.Function)
	require.Equal(t, "aes-256-gcm", file.Crypto.Cipher.Function)
	require.NotEmpty(t, file.Crypto.KDF.Params.Salt)
	require.NotEmpty(t, file.Crypto.Cipher.Params.IV)
	require.NotEmpty(t, file.Crypto.Cipher.Message)

	secret, err := decryptSecret(file.Crypto, KeystorePassword)
	require.NoError(t, err)
	require.Equal(t, key.Seed(), secret)

	file.Crypto.KDF.Params.T++
	_, err = decryptSecret(file.Crypto, KeystorePassword)
	require.Error(t, err)

	_, err = key.KeystoreJSON("")
	require.ErrorContains(t, err, "keystore password is required")
}
