package validatorops

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// qrysmKeystore is the keystore Qrysm's deposit CLI writes, with the light
// KDF, for the seed 0x91, 0x92, ..., less its public key and path.
const qrysmKeystore = `{"crypto":{"kdf":{"function":"argon2id","params":{"dklen":32,"m":4096,"p":1,` +
	`"salt":"5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a","t":8},"message":""},"checksum":null,` +
	`"cipher":{"function":"aes-256-gcm","params":{"iv":"a5a5a5a5a5a5a5a5a5a5a5a5"},` +
	`"message":"ba9fa934f613407f83751f6e4b361336036c5707c2dc2f9429cfbaae83e5d25a48f51c274076ff70a6acadcab2f4d513cd1a5d2a90b1aac673790c92feba2d8a"}},` +
	`"version":1}`

func TestKeyFromKeystore(t *testing.T) {
	want, err := DeterministicKey(0x91)
	require.NoError(t, err)

	key, err := KeyFromKeystore([]byte(qrysmKeystore), KeystorePassword)
	require.NoError(t, err)
	require.Equal(t, want.PublicKey(), key.PublicKey())

	encoded, err := want.KeystoreJSON(KeystorePassword)
	require.NoError(t, err)
	key, err = KeyFromKeystore([]byte(encoded), KeystorePassword)
	require.NoError(t, err)
	require.Equal(t, want.PublicKey(), key.PublicKey())

	_, err = KeyFromKeystore([]byte(qrysmKeystore), "wrong")
	require.ErrorContains(t, err, "decrypt keystore")
	_, err = KeyFromKeystore([]byte(`{"crypto":{"kdf":{"function":"scrypt"},"cipher":{"function":"aes-128-ctr"}}}`), KeystorePassword)
	require.EqualError(t, err, "keystore uses scrypt and aes-128-ctr, want argon2id and aes-256-gcm")
}

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
