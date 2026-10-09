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

const (
	// depositEntryJSON is one entry of the deposit data file the CLI writes.
	depositEntryJSON = `{
		"pubkey":"0xabc",
		"amount":40000000000000,
		"withdrawal_recipient":"0xdef",
		"randao_commitment":"0x123",
		"fork_version":"0x10000038"
	}`
	keystoreName = "keystore-m_12381_238_0_0-1.json"
	keystoreJSON = `{"crypto":{}}`
)

func TestParseDepositOutput(t *testing.T) {
	result, err := parseDepositOutput([]sidecar.File{
		{Name: "keys/deposit_data-1.json", Body: []byte("[" + depositEntryJSON + "]")},
		{Name: "keys/" + keystoreName, Body: []byte(keystoreJSON)},
		{Name: "keys/readme.txt", Body: []byte("ignore")},
	})
	require.NoError(t, err)
	require.Equal(t, "0xabc", result.PublicKey)
	require.Equal(t, uint64(40000000000000), result.Amount)
	require.Equal(t, "0xdef", result.WithdrawalRecipient)
	require.Equal(t, "0x123", result.RandaoCommitment)
	require.Equal(t, "0x10000038", result.ForkVersion)
	require.Equal(t, []sidecar.File{keystoreFile()}, result.Keystores)
}

func TestParseDepositOutputRejectsMissingFields(t *testing.T) {
	for name, entry := range map[string]string{
		"pubkey":               `{"amount":1,"withdrawal_recipient":"0xdef","randao_commitment":"0x123","fork_version":"0x10000038"}`,
		"amount":               `{"pubkey":"0xabc","withdrawal_recipient":"0xdef","randao_commitment":"0x123","fork_version":"0x10000038"}`,
		"withdrawal recipient": `{"pubkey":"0xabc","amount":1,"randao_commitment":"0x123","fork_version":"0x10000038"}`,
		"RANDAO commitment":    `{"pubkey":"0xabc","amount":1,"withdrawal_recipient":"0xdef","fork_version":"0x10000038"}`,
		"fork version":         `{"pubkey":"0xabc","amount":1,"withdrawal_recipient":"0xdef","randao_commitment":"0x123"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseDepositOutput([]sidecar.File{depositDataFile("[" + entry + "]"), keystoreFile()})
			require.ErrorContains(t, err, "deposit data is missing")
		})
	}
}

func TestParseDepositOutputRejectsUnexpectedOutput(t *testing.T) {
	data := depositDataFile("[" + depositEntryJSON + "]")
	for name, test := range map[string]struct {
		files []sidecar.File
		want  string
	}{
		"no files":               {nil, "expected one deposit data file, found 0"},
		"two deposit data files": {[]sidecar.File{data, data, keystoreFile()}, "expected one deposit data file, found 2"},
		"no keystore":            {[]sidecar.File{data}, "expected one keystore, found 0"},
		"two keystores":          {[]sidecar.File{data, keystoreFile(), keystoreFile()}, "expected one keystore, found 2"},
		"no entries":             {[]sidecar.File{depositDataFile("[]"), keystoreFile()}, "expected one deposit data entry, found 0"},
		"two entries": {
			[]sidecar.File{depositDataFile("[" + depositEntryJSON + "," + depositEntryJSON + "]"), keystoreFile()},
			"expected one deposit data entry, found 2",
		},
		"invalid JSON": {[]sidecar.File{depositDataFile("{"), keystoreFile()}, "decode deposit data"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseDepositOutput(test.files)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestFixtureFiles(t *testing.T) {
	files := fixtureFiles(Config{Password: "staker-e2e", PayerSeed: " aa\n"})
	names := make([]string, len(files))
	for index, file := range files {
		names[index] = file.Name
	}
	require.Equal(t, []string{"/run-deposit.sh", "/keystore-password.txt", "/payer.seed"}, names)
	require.Equal(t, []byte("staker-e2e"), files[1].Body)
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

func TestStartScriptKeepsTheSeedOutOfTheLog(t *testing.T) {
	// new-seed prints the new key's seed and mnemonic, and the container's log
	// ends up in errors and failure reports.
	newSeed, submit, found := strings.Cut(depositStartScript, "/usr/local/bin/deposit submit")
	require.True(t, found)
	require.Contains(t, newSeed, `--lightkdf >"${NEW_SEED_LOG}" 2>&1`)
	require.NotContains(t, submit, "NEW_SEED_LOG")
}

func TestValidateConfig(t *testing.T) {
	require.NoError(t, validateConfig(testConfig()))

	for want, clearField := range map[string]func(*Config){
		"deposit CLI image is empty":        func(config *Config) { config.Image = " " },
		"execution RPC URL is empty":        func(config *Config) { config.ExecutionRPCURL = "" },
		"deposit contract address is empty": func(config *Config) { config.DepositContract = "" },
		"withdrawal recipient is empty":     func(config *Config) { config.WithdrawalRecipient = "" },
		"payer seed is empty":               func(config *Config) { config.PayerSeed = "" },
		"keystore password is empty":        func(config *Config) { config.Password = "" },
	} {
		t.Run(want, func(t *testing.T) {
			config := testConfig()
			clearField(&config)
			require.EqualError(t, validateConfig(config), want)
		})
	}
}

func TestRunReadsDepositOutput(t *testing.T) {
	docker := dockerWithDepositOutput()
	docker.Logs = "Successfully sent all validator deposits!\n"

	result, err := run(t.Context(), testConfig(), docker)
	require.NoError(t, err)
	require.Equal(t, "0xabc", result.PublicKey)
	require.Len(t, result.Keystores, 1)
	require.Equal(t, "Successfully sent all validator deposits!", result.Output)

	created := docker.Created.Config
	require.Equal(t, "deposit:test", created.Image)
	require.Equal(t, []string{"/bin/sh", "/run-deposit.sh"}, created.Entrypoint)
	require.Equal(t, []string{
		"KEYS_DIR=/keys",
		"PASSWORD_FILE=/keystore-password.txt",
		"SEED_FILE=/payer.seed",
		"NEW_SEED_LOG=/new-seed.log",
		"CHAIN_NAME=dev",
		"DEPOSIT_EXECUTION_ADDRESS=Q1111111111111111111111111111111111111111",
		"DEPOSIT_EL_RPC=http://host.docker.internal:8545",
		"DEPOSIT_CONTRACT=Q4242424242424242424242424242424242424242",
	}, created.Env)
	require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
}

func TestRunSucceedsWithoutTheOutput(t *testing.T) {
	docker := dockerWithDepositOutput()
	docker.Fail["ContainerLogs"] = errors.New("daemon unavailable")

	result, err := run(t.Context(), testConfig(), docker)
	require.NoError(t, err)
	require.Equal(t, "0xabc", result.PublicKey)
	require.Equal(t, "(logs unavailable: daemon unavailable)", result.Output)
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

func TestRunReportsUnreadableOutput(t *testing.T) {
	// The fake has no files, so copying the keys directory fails.
	docker := sidecartest.NewDocker()

	_, err := run(t.Context(), testConfig(), docker)
	require.ErrorContains(t, err, "copy deposit CLI output")
	require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
}

func depositDataFile(body string) sidecar.File {
	return sidecar.File{Name: "deposit_data-1.json", Body: []byte(body)}
}

func keystoreFile() sidecar.File {
	return sidecar.File{Name: keystoreName, Body: []byte(keystoreJSON)}
}

func dockerWithDepositOutput() *sidecartest.Docker {
	docker := sidecartest.NewDocker()
	docker.Files["/keys/deposit_data-1.json"] = []byte("[" + depositEntryJSON + "]")
	docker.Files["/keys/"+keystoreName] = []byte(keystoreJSON)
	return docker
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
