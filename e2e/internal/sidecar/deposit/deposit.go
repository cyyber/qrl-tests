// Package deposit runs the Qrysm staking-deposit-cli in a sidecar against a
// live development network.
package deposit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/internal/dockerapi"
)

const (
	sidecarName     = "deposit CLI"
	chainName       = "dev"
	startScriptPath = "/run-deposit.sh"
	passwordPath    = "/keystore-password.txt"
	seedPath        = "/payer.seed"
	keysDir         = "/keys"
	newSeedLogPath  = "/new-seed.log"

	depositDataFilePrefix = "deposit_data-"
	keystoreFilePrefix    = "keystore-"

	// outputLines is how much of the submit step's output a Result carries.
	outputLines = 50
)

// Result is what one run of the deposit CLI wrote: the deposit it signed and
// the keystore of the new key.
type Result struct {
	PublicKey           string
	Amount              uint64
	WithdrawalRecipient string
	RandaoCommitment    string
	// ForkVersion is the fork version the deposit is signed for.
	ForkVersion string
	Keystores   []sidecar.File
	// Output is the end of what the submit step printed. The CLI exits 0 even
	// when a send fails, and only its output says why.
	Output string
}

// Config describes one new-seed and submit run of the deposit CLI.
type Config struct {
	// Image is the Qrysm all-tools image, which carries the deposit CLI.
	Image               string
	ExecutionRPCURL     string
	DepositContract     string
	WithdrawalRecipient string
	PayerSeed           string
	Password            string
}

// Run generates one validator keystore and submits the compiled-in maximum
// deposit from the development wallet.
func Run(ctx context.Context, config Config) (Result, error) {
	client, err := dockerapi.New()
	if err != nil {
		return Result{}, fmt.Errorf("create Docker client: %w", err)
	}
	defer func() { _ = client.Close() }()
	return run(ctx, config, client)
}

func run(ctx context.Context, config Config, client sidecar.Client) (Result, error) {
	if err := validateConfig(config); err != nil {
		return Result{}, err
	}
	executionRPC, err := sidecar.HostURL(config.ExecutionRPCURL)
	if err != nil {
		return Result{}, fmt.Errorf("rewrite execution RPC URL: %w", err)
	}

	container, err := sidecar.Run(ctx, client, sidecar.Spec{
		Name:       sidecarName,
		Image:      config.Image,
		Entrypoint: []string{"/bin/sh", startScriptPath},
		Env:        containerEnv(config, executionRPC),
		Files:      fixtureFiles(config),
	})
	if err != nil {
		return Result{}, err
	}

	files, err := container.ReadDir(ctx, keysDir)
	output, outputErr := container.Logs(outputLines)
	if err = errors.Join(err, container.Close()); err != nil {
		return Result{}, fmt.Errorf("copy deposit CLI output: %w", err)
	}
	result, err := parseDepositOutput(files)
	if err != nil {
		return Result{}, err
	}
	result.Output = output
	if outputErr != nil {
		result.Output = fmt.Sprintf("(logs unavailable: %v)", outputErr)
	}
	return result, nil
}

type depositData struct {
	PubKey              string `json:"pubkey"`
	Amount              uint64 `json:"amount"`
	WithdrawalRecipient string `json:"withdrawal_recipient"`
	RandaoCommitment    string `json:"randao_commitment"`
	ForkVersion         string `json:"fork_version"`
}

func parseDepositOutput(files []sidecar.File) (Result, error) {
	var (
		dataFiles []sidecar.File
		keystores []sidecar.File
	)
	for _, file := range files {
		name := path.Base(file.Name)
		switch {
		case strings.HasPrefix(name, depositDataFilePrefix) && strings.HasSuffix(name, ".json"):
			dataFiles = append(dataFiles, sidecar.File{Name: name, Body: file.Body})
		case strings.HasPrefix(name, keystoreFilePrefix) && strings.HasSuffix(name, ".json"):
			keystores = append(keystores, sidecar.File{Name: name, Body: file.Body})
		}
	}
	if len(dataFiles) != 1 {
		return Result{}, fmt.Errorf("expected one deposit data file, found %d", len(dataFiles))
	}
	if len(keystores) != 1 {
		return Result{}, fmt.Errorf("expected one keystore, found %d", len(keystores))
	}

	var entries []depositData
	if err := json.Unmarshal(dataFiles[0].Body, &entries); err != nil {
		return Result{}, fmt.Errorf("decode deposit data: %w", err)
	}
	if len(entries) != 1 {
		return Result{}, fmt.Errorf("expected one deposit data entry, found %d", len(entries))
	}
	entry := entries[0]
	if entry.PubKey == "" || entry.WithdrawalRecipient == "" || entry.RandaoCommitment == "" ||
		entry.ForkVersion == "" || entry.Amount == 0 {
		return Result{}, errors.New(
			"deposit data is missing pubkey, withdrawal recipient, RANDAO commitment, fork version, or amount")
	}
	return Result{
		PublicKey:           entry.PubKey,
		Amount:              entry.Amount,
		WithdrawalRecipient: entry.WithdrawalRecipient,
		RandaoCommitment:    entry.RandaoCommitment,
		ForkVersion:         entry.ForkVersion,
		Keystores:           keystores,
	}, nil
}

func validateConfig(config Config) error {
	switch {
	case strings.TrimSpace(config.Image) == "":
		return errors.New("deposit CLI image is empty")
	case strings.TrimSpace(config.ExecutionRPCURL) == "":
		return errors.New("execution RPC URL is empty")
	case strings.TrimSpace(config.DepositContract) == "":
		return errors.New("deposit contract address is empty")
	case strings.TrimSpace(config.WithdrawalRecipient) == "":
		return errors.New("withdrawal recipient is empty")
	case strings.TrimSpace(config.PayerSeed) == "":
		return errors.New("payer seed is empty")
	case strings.TrimSpace(config.Password) == "":
		return errors.New("keystore password is empty")
	default:
		return nil
	}
}

func fixtureFiles(config Config) []sidecar.File {
	return []sidecar.File{
		{Name: startScriptPath, Body: []byte(depositStartScript), Mode: 0o755},
		{Name: passwordPath, Body: []byte(config.Password)},
		{Name: seedPath, Body: []byte(strings.TrimSpace(config.PayerSeed))},
	}
}

func containerEnv(config Config, executionRPC string) []string {
	return []string{
		"KEYS_DIR=" + keysDir,
		"PASSWORD_FILE=" + passwordPath,
		"SEED_FILE=" + seedPath,
		"NEW_SEED_LOG=" + newSeedLogPath,
		"CHAIN_NAME=" + chainName,
		"DEPOSIT_EXECUTION_ADDRESS=" + config.WithdrawalRecipient,
		"DEPOSIT_EL_RPC=" + executionRPC,
		"DEPOSIT_CONTRACT=" + config.DepositContract,
	}
}

// depositStartScript reads its paths from the environment set by
// containerEnv, so the Go constants stay the single source of truth.
//
// new-seed prints the new key's seed and mnemonic. Its output stays out of the
// container's log, which errors and failure reports quote, unless it fails.
const depositStartScript = `#!/bin/sh
set -eu
if ! /usr/local/bin/deposit new-seed \
  --num-validators=1 \
  --folder="${KEYS_DIR}" \
  --chain-name="${CHAIN_NAME}" \
  --execution-address="${DEPOSIT_EXECUTION_ADDRESS}" \
  --keystore-password-file="${PASSWORD_FILE}" \
  --lightkdf >"${NEW_SEED_LOG}" 2>&1; then
  cat "${NEW_SEED_LOG}" >&2
  exit 1
fi
/usr/local/bin/deposit submit \
  --validator-keys-dir="${KEYS_DIR}" \
  --seed-file="${SEED_FILE}" \
  --http-web3provider="${DEPOSIT_EL_RPC}" \
  --deposit-contract="${DEPOSIT_CONTRACT}" \
  --skip-deposit-confirmation \
  --deposit-delay-seconds=0
`
