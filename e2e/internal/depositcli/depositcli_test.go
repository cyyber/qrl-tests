package depositcli

import (
	"bytes"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/operatorvc"
	"github.com/stretchr/testify/require"
)

func TestImageFromEnv(t *testing.T) {
	t.Setenv(ImageEnv, "")
	require.Equal(t, DefaultImage, ImageFromEnv())

	t.Setenv(ImageEnv, " registry.example/qrysm-deposit:dev ")
	require.Equal(t, "registry.example/qrysm-deposit:dev", ImageFromEnv())
}

func TestParseDepositOutput(t *testing.T) {
	result, err := parseDepositOutput([]operatorvc.File{
		{Name: "keys/deposit_data-1.json", Body: []byte(`[{
			"pubkey":"0xabc",
			"amount":40000000000000,
			"withdrawal_recipient":"0xdef",
			"randao_commitment":"0x123"
		}]`)},
		{Name: "keys/keystore-m_12381_238_0_0-1.json", Body: []byte(`{"crypto":{}}`)},
		{Name: "keys/readme.txt", Body: []byte("ignore")},
	})
	require.NoError(t, err)
	require.Equal(t, "0xabc", result.PublicKey)
	require.Equal(t, uint64(40000000000000), result.Amount)
	require.Equal(t, "0xdef", result.WithdrawalRecipient)
	require.Equal(t, "0x123", result.RandaoCommitment)
	require.Equal(t, []operatorvc.File{{
		Name: "keystore-m_12381_238_0_0-1.json",
		Body: []byte(`{"crypto":{}}`),
	}}, result.Keystores)
}

func TestParseDepositOutputRejectsUnexpectedCounts(t *testing.T) {
	_, err := parseDepositOutput(nil)
	require.ErrorContains(t, err, "deposit data file")

	_, err = parseDepositOutput([]operatorvc.File{
		{Name: "deposit_data-1.json", Body: []byte(`[{
			"pubkey":"0xabc","amount":1,"withdrawal_recipient":"0xdef"
		}]`)},
	})
	require.ErrorContains(t, err, "one keystore")
}

func TestDepositFixtureArchive(t *testing.T) {
	archive, err := depositFixtureArchive(Config{
		Password:  "staker-e2e",
		PayerSeed: "aa",
	})
	require.NoError(t, err)
	files, err := readTarFiles(bytes.NewReader(archive))
	require.NoError(t, err)
	names := make([]string, len(files))
	for index, file := range files {
		names[index] = file.Name
	}
	require.Equal(t, []string{"run-deposit.sh", "keystore-password.txt", "payer.seed"}, names)
}

func TestValidateConfig(t *testing.T) {
	err := validateConfig(Config{})
	require.ErrorContains(t, err, "image")
}
