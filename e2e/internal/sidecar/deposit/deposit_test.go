package deposit

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/sidecartest"
	"github.com/stretchr/testify/require"
)

var _ dockerClient = (*sidecartest.Docker)(nil)

func TestImageFromEnv(t *testing.T) {
	t.Setenv(ImageEnv, "")
	require.Equal(t, DefaultImage, ImageFromEnv())

	t.Setenv(ImageEnv, " registry.example/qrysm-deposit:dev ")
	require.Equal(t, "registry.example/qrysm-deposit:dev", ImageFromEnv())
}

func TestParseDepositOutput(t *testing.T) {
	result, err := parseDepositOutput([]sidecar.File{
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
	require.Equal(t, "0x123", result.RandaoCommitment)
	require.Equal(t, []sidecar.File{{
		Name: "keystore-m_12381_238_0_0-1.json",
		Body: []byte(`{"crypto":{}}`),
	}}, result.Keystores)
}

func TestParseDepositOutputRejectsUnexpectedCounts(t *testing.T) {
	_, err := parseDepositOutput(nil)
	require.ErrorContains(t, err, "deposit data file")

	_, err = parseDepositOutput([]sidecar.File{
		{Name: "deposit_data-1.json", Body: []byte(`[{
			"pubkey":"0xabc","amount":1,"withdrawal_recipient":"0xdef"
		}]`)},
	})
	require.ErrorContains(t, err, "one keystore")
}

func TestFixtureFiles(t *testing.T) {
	files := fixtureFiles(Config{Password: "staker-e2e", PayerSeed: " aa\n"})
	names := make([]string, len(files))
	for index, file := range files {
		names[index] = file.Name
	}
	require.Equal(t, []string{"/run-deposit.sh", "/keystore-password.txt", "/payer.seed"}, names)
	require.Equal(t, []byte("aa"), files[2].Body)
}

func TestStartScriptVariablesAreSet(t *testing.T) {
	set := map[string]bool{}
	for _, variable := range containerEnv(testConfig(), "http://host.docker.internal:8545") {
		name, _, _ := strings.Cut(variable, "=")
		set[name] = true
	}
	for _, match := range regexp.MustCompile(`\$\{([A-Z_]+)\}`).FindAllStringSubmatch(depositStartScript, -1) {
		require.True(t, set[match[1]], "start script reads %s, which containerEnv does not set", match[1])
	}
}

func TestValidateConfig(t *testing.T) {
	err := validateConfig(Config{})
	require.ErrorContains(t, err, "image")
}

func TestRunReadsDepositOutput(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.Files["/keys/deposit_data-1.json"] = []byte(`[{"pubkey":"0xabc","amount":40000000000000,"withdrawal_recipient":"0xdef"}]`)
	docker.Files["/keys/keystore-m_12381_238_0_0-1.json"] = []byte(`{"crypto":{}}`)

	result, err := run(t.Context(), testConfig(), docker)
	require.NoError(t, err)
	require.Equal(t, "0xabc", result.PublicKey)
	require.Len(t, result.Keystores, 1)
	require.Contains(t, docker.Created.Config.Env, "DEPOSIT_EL_RPC=http://host.docker.internal:8545")
	require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
}

func TestRunReportsFailedDeposit(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.ExitCode = 1
	docker.Logs = "failed to connect to the qrl provider"

	_, err := run(t.Context(), testConfig(), docker)
	require.ErrorContains(t, err, "deposit CLI container exited with code 1")
	require.ErrorContains(t, err, "failed to connect to the qrl provider")
	require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
}

func TestRunRequiresTheImage(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.Fail["ImageInspect"] = errors.New("no such image")

	_, err := run(t.Context(), testConfig(), docker)
	require.ErrorIs(t, err, docker.Fail["ImageInspect"])
	require.ErrorContains(t, err, `deposit CLI image "deposit:test" is not available (build it with make deposit-image)`)
	require.Nil(t, docker.Created.Config, "no container is created without the image")
}

func testConfig() Config {
	return Config{
		Image:               "deposit:test",
		ExecutionRPCURL:     "http://127.0.0.1:8545",
		DepositContract:     "Q4242424242424242424242424242424242424242",
		WithdrawalRecipient: "Q1111111111111111111111111111111111111111",
		PayerSeed:           "aa",
		Password:            "staker-e2e",
	}
}
