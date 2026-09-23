// Package deposit runs the Qrysm staking-deposit-cli in a sidecar against a
// live development network.
package deposit

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/internal/dockerapi"
	dockerclient "github.com/moby/moby/client"
)

const (
	// ImageEnv selects the locally built deposit CLI image.
	ImageEnv = "DEVNET_DEPOSIT_IMAGE"
	// DefaultImage is the local tag produced by `make deposit-image`.
	DefaultImage = "local/qrysm-deposit:devnet"

	sidecarName     = "deposit CLI"
	chainName       = "dev"
	startScriptPath = "/run-deposit.sh"
	passwordPath    = "/keystore-password.txt"
	seedPath        = "/payer.seed"
	keysDir         = "/keys"
)

type Result struct {
	PublicKey        string
	Amount           uint64
	RandaoCommitment string
	Keystores        []sidecar.File
}

// Config describes one new-seed and submit run of the deposit CLI.
type Config struct {
	Image               string
	ExecutionRPCURL     string
	DepositContract     string
	WithdrawalRecipient string
	PayerSeed           string
	Password            string
}

func ImageFromEnv() string {
	return cmp.Or(strings.TrimSpace(os.Getenv(ImageEnv)), DefaultImage)
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

type dockerClient interface {
	sidecar.Client
	ImageInspect(context.Context, string, ...dockerclient.ImageInspectOption) (dockerclient.ImageInspectResult, error)
}

func run(ctx context.Context, config Config, client dockerClient) (Result, error) {
	if err := validateConfig(config); err != nil {
		return Result{}, err
	}
	if _, err := client.ImageInspect(ctx, config.Image); err != nil {
		return Result{}, fmt.Errorf("deposit CLI image %q is not available (build it with make deposit-image): %w", config.Image, err)
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
	if err = errors.Join(err, container.Close()); err != nil {
		return Result{}, fmt.Errorf("copy deposit CLI output: %w", err)
	}
	return parseDepositOutput(files)
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
		"CHAIN_NAME=" + chainName,
		"DEPOSIT_EXECUTION_ADDRESS=" + config.WithdrawalRecipient,
		"DEPOSIT_EL_RPC=" + executionRPC,
		"DEPOSIT_CONTRACT=" + config.DepositContract,
	}
}

// depositStartScript reads its paths from the environment set by
// containerEnv, so the Go constants stay the single source of truth.
const depositStartScript = `#!/bin/sh
set -eu
/usr/local/bin/deposit new-seed \
  --num-validators=1 \
  --folder="${KEYS_DIR}" \
  --chain-name="${CHAIN_NAME}" \
  --execution-address="${DEPOSIT_EXECUTION_ADDRESS}" \
  --keystore-password-file="${PASSWORD_FILE}" \
  --lightkdf
/usr/local/bin/deposit submit \
  --validator-keys-dir="${KEYS_DIR}" \
  --seed-file="${SEED_FILE}" \
  --http-web3provider="${DEPOSIT_EL_RPC}" \
  --deposit-contract="${DEPOSIT_CONTRACT}" \
  --skip-deposit-confirmation \
  --deposit-delay-seconds=0
`
