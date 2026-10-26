package deposit

import (
	"regexp"
	"strings"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/sidecartest"
	"github.com/stretchr/testify/require"
)

func TestGenesisKeyReadsTheKeystore(t *testing.T) {
	docker := dockerWithDepositOutput()
	docker.Logs = "new-seed output"

	result, err := genesisKey(t.Context(), genesisKeyTestConfig(), docker)
	require.NoError(t, err)
	require.Equal(t, "0xabc", result.PublicKey)
	require.Len(t, result.Keystores, 1)
	require.Empty(t, result.Output)

	created := docker.LastCreated().Config
	require.Equal(t, "deposit:test", created.Image)
	require.Equal(t, []string{"/bin/sh", "/derive-genesis-key.sh"}, created.Entrypoint)
	require.Equal(t, []string{
		"KEYS_DIR=/keys",
		"PASSWORD_FILE=/keystore-password.txt",
		"NEW_SEED_LOG=/new-seed.log",
		"CHAIN_NAME=dev",
		"DEPOSIT_EXECUTION_ADDRESS=Q1111111111111111111111111111111111111111",
		"MNEMONIC=veto waiter rail",
		"VALIDATOR_INDEX=64",
	}, created.Env)
	require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
}

func TestGenesisKeyReportsAFailedRun(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.ExitCode = 1
	docker.Logs = "invalid mnemonic"

	_, err := genesisKey(t.Context(), genesisKeyTestConfig(), docker)
	require.ErrorContains(t, err, "deposit CLI container exited with code 1")
	require.ErrorContains(t, err, "invalid mnemonic")
}

func TestGenesisKeyScriptVariablesAreSet(t *testing.T) {
	set := map[string]bool{}
	for _, variable := range genesisKeyEnv(genesisKeyTestConfig()) {
		name, _, _ := strings.Cut(variable, "=")
		set[name] = true
	}
	for _, match := range regexp.MustCompile(`\$\{([A-Z_]+)\}`).FindAllStringSubmatch(genesisKeyScript, -1) {
		require.True(t, set[match[1]], "script reads %s, which genesisKeyEnv does not set", match[1])
	}
}

func TestValidateGenesisKeyConfig(t *testing.T) {
	require.NoError(t, validateGenesisKeyConfig(genesisKeyTestConfig()))

	for want, clearField := range map[string]func(*GenesisKeyConfig){
		"deposit CLI image is empty":    func(config *GenesisKeyConfig) { config.Image = " " },
		"mnemonic is empty":             func(config *GenesisKeyConfig) { config.Mnemonic = "" },
		"withdrawal recipient is empty": func(config *GenesisKeyConfig) { config.WithdrawalRecipient = "" },
		"keystore password is empty":    func(config *GenesisKeyConfig) { config.Password = "" },
	} {
		t.Run(want, func(t *testing.T) {
			config := genesisKeyTestConfig()
			clearField(&config)
			require.EqualError(t, validateGenesisKeyConfig(config), want)
		})
	}
}

func genesisKeyTestConfig() GenesisKeyConfig {
	return GenesisKeyConfig{
		Image:               "deposit:test",
		Mnemonic:            " veto waiter rail ",
		Index:               64,
		WithdrawalRecipient: "Q1111111111111111111111111111111111111111",
		Password:            "staker-e2e",
	}
}
