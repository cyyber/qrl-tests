package deposit

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/internal/dockerapi"
)

const genesisStartScriptPath = "/derive-genesis-key.sh"

// GenesisKeyConfig describes one new-seed run that derives the key of a
// genesis validator.
type GenesisKeyConfig struct {
	// Image is the Qrysm all-tools image, which carries the deposit CLI.
	Image string
	// Mnemonic is the mnemonic the network's genesis validators derive from.
	Mnemonic string
	// Index is the validator's genesis index.
	Index uint64
	// WithdrawalRecipient is required by new-seed. Only the deposit data,
	// which nothing submits, carries it.
	WithdrawalRecipient string
	Password            string
}

// GenesisKey derives the keystore of the genesis validator at config.Index
// the way qrl-package derives the keystores its validator clients run. It
// sends nothing; Result.Output is empty.
func GenesisKey(ctx context.Context, config GenesisKeyConfig) (Result, error) {
	client, err := dockerapi.New()
	if err != nil {
		return Result{}, fmt.Errorf("create Docker client: %w", err)
	}
	defer func() { _ = client.Close() }()
	return genesisKey(ctx, config, client)
}

func genesisKey(ctx context.Context, config GenesisKeyConfig, client sidecar.Client) (Result, error) {
	if err := validateGenesisKeyConfig(config); err != nil {
		return Result{}, err
	}
	result, err := runCLI(ctx, client, sidecar.Spec{
		Name:       sidecarName,
		Image:      config.Image,
		Entrypoint: []string{"/bin/sh", genesisStartScriptPath},
		Env:        genesisKeyEnv(config),
		Files: []sidecar.File{
			{Name: genesisStartScriptPath, Body: []byte(genesisKeyScript), Mode: 0o755},
			{Name: passwordPath, Body: []byte(config.Password)},
		},
	})
	result.Output = ""
	return result, err
}

func validateGenesisKeyConfig(config GenesisKeyConfig) error {
	switch {
	case strings.TrimSpace(config.Image) == "":
		return errors.New("deposit CLI image is empty")
	case strings.TrimSpace(config.Mnemonic) == "":
		return errors.New("mnemonic is empty")
	case strings.TrimSpace(config.WithdrawalRecipient) == "":
		return errors.New("withdrawal recipient is empty")
	case strings.TrimSpace(config.Password) == "":
		return errors.New("keystore password is empty")
	default:
		return nil
	}
}

func genesisKeyEnv(config GenesisKeyConfig) []string {
	return []string{
		"KEYS_DIR=" + keysDir,
		"PASSWORD_FILE=" + passwordPath,
		"NEW_SEED_LOG=" + newSeedLogPath,
		"CHAIN_NAME=" + chainName,
		"DEPOSIT_EXECUTION_ADDRESS=" + config.WithdrawalRecipient,
		"MNEMONIC=" + strings.TrimSpace(config.Mnemonic),
		fmt.Sprintf("VALIDATOR_INDEX=%d", config.Index),
	}
}

// genesisKeyScript mirrors the keystore generation of the pinned qrl-package.
// Like depositStartScript, it keeps new-seed's output out of the container's
// log unless it fails.
const genesisKeyScript = `#!/bin/sh
set -eu
if ! /usr/local/bin/deposit new-seed \
  --validator-start-index="${VALIDATOR_INDEX}" \
  --num-validators=1 \
  --mnemonic="${MNEMONIC}" \
  --folder="${KEYS_DIR}" \
  --chain-name="${CHAIN_NAME}" \
  --execution-address="${DEPOSIT_EXECUTION_ADDRESS}" \
  --keystore-password-file="${PASSWORD_FILE}" \
  --lightkdf >"${NEW_SEED_LOG}" 2>&1; then
  cat "${NEW_SEED_LOG}" >&2
  exit 1
fi
`
