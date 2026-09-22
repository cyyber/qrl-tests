package validatorops

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/crypto/argon2"
)

const (
	// KeystorePassword encrypts the imported keystore. It is a test fixture,
	// not a secret.
	KeystorePassword = "staker-e2e"

	argon2Time    = 1
	argon2Memory  = 1024
	argon2Threads = 1
	argon2KeyLen  = 32
	keystoreSalt  = 32
	keystoreIV    = 12
)

type keystoreFile struct {
	Crypto      keystoreCrypto `json:"crypto"`
	UUID        string         `json:"uuid"`
	Pubkey      string         `json:"pubkey"`
	Version     uint           `json:"version"`
	Description string         `json:"description"`
}

type keystoreCrypto struct {
	KDF    keystoreKDF    `json:"kdf"`
	Cipher keystoreCipher `json:"cipher"`
}

type keystoreKDF struct {
	Function string            `json:"function"`
	Params   keystoreKDFParams `json:"params"`
}

type keystoreKDFParams struct {
	Salt  string `json:"salt"`
	DKLen int    `json:"dklen"`
	T     int    `json:"t"`
	M     int    `json:"m"`
	P     int    `json:"p"`
}

type keystoreCipher struct {
	Function string               `json:"function"`
	Params   keystoreCipherParams `json:"params"`
	Message  string               `json:"message"`
}

type keystoreCipherParams struct {
	IV string `json:"iv"`
}

// KeystoreJSON encrypts the key's seed in the Qrysm local-wallet format so
// the validator client can import it through the keymanager API.
func (key *Key) KeystoreJSON(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("keystore password is required")
	}
	cryptoFields, err := encryptSecret(key.Seed(), password)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(keystoreFile{
		Crypto:      cryptoFields,
		UUID:        uuid.NewString(),
		Pubkey:      hex.EncodeToString(key.PublicKey()),
		Version:     1,
		Description: "keystore",
	})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func encryptSecret(secret []byte, password string) (keystoreCrypto, error) {
	salt := make([]byte, keystoreSalt)
	if _, err := rand.Read(salt); err != nil {
		return keystoreCrypto{}, fmt.Errorf("keystore salt: %w", err)
	}
	iv := make([]byte, keystoreIV)
	if _, err := rand.Read(iv); err != nil {
		return keystoreCrypto{}, fmt.Errorf("keystore iv: %w", err)
	}

	params := keystoreKDFParams{
		Salt:  hex.EncodeToString(salt),
		DKLen: argon2KeyLen,
		T:     argon2Time,
		M:     argon2Memory,
		P:     argon2Threads,
	}
	key, err := argon2Key([]byte(password), salt, params)
	if err != nil {
		return keystoreCrypto{}, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return keystoreCrypto{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return keystoreCrypto{}, err
	}

	return keystoreCrypto{
		KDF: keystoreKDF{
			Function: "argon2id",
			Params:   params,
		},
		Cipher: keystoreCipher{
			Function: "aes-256-gcm",
			Params:   keystoreCipherParams{IV: hex.EncodeToString(iv)},
			Message:  hex.EncodeToString(gcm.Seal(nil, iv, secret, nil)),
		},
	}, nil
}

func decryptSecret(crypto keystoreCrypto, password string) ([]byte, error) {
	salt, err := hex.DecodeString(crypto.KDF.Params.Salt)
	if err != nil {
		return nil, fmt.Errorf("keystore salt: %w", err)
	}
	iv, err := hex.DecodeString(crypto.Cipher.Params.IV)
	if err != nil {
		return nil, fmt.Errorf("keystore iv: %w", err)
	}
	message, err := hex.DecodeString(crypto.Cipher.Message)
	if err != nil {
		return nil, fmt.Errorf("keystore message: %w", err)
	}
	key, err := argon2Key([]byte(password), salt, crypto.KDF.Params)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, iv, message, nil)
}

func argon2Key(password, salt []byte, params keystoreKDFParams) ([]byte, error) {
	if params.T < 1 || params.M < 1 || params.P < 1 || params.P > 255 || params.DKLen < 1 {
		return nil, fmt.Errorf("invalid keystore KDF params")
	}
	return argon2.IDKey(password, salt, uint32(params.T), uint32(params.M), uint8(params.P), uint32(params.DKLen)), nil
}
