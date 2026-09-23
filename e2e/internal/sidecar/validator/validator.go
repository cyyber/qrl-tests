// Package validator runs a Qrysm validator client in a sidecar container.
package validator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/keymanager"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
	"github.com/cyyber/qrl-tests/internal/dockerapi"
)

const (
	sidecarName       = "validator sidecar"
	readyPollInterval = 500 * time.Millisecond
	gatewayPort       = 7500
	walletDir         = "/wallet"
	keysDir           = "/keys"
	passwordPath      = "/wallet-password.txt"
	chainConfigPath   = "/network-configs/config.yaml"
	startScriptPath   = "/start-validator.sh"
	authTokenPath     = walletDir + "/auth-token"
	walletPassword    = validatorops.KeystorePassword
)

// startScript reads its settings from the environment set by containerEnv, so
// the Go constants stay the single source of truth.
const startScript = `#!/bin/sh
set -eu
set -- --accept-terms-of-use --wallet-dir="${WALLET_DIR}" --wallet-password-file="${PASSWORD_FILE}"

/validator wallet create "$@" --keymanager-kind=imported

if [ -d "${KEYS_DIR}" ]; then
  /validator accounts import "$@" \
    --keys-dir="${KEYS_DIR}" \
    --account-password-file="${PASSWORD_FILE}"
fi

exec /validator "$@" \
  --chain-config-file="${CHAIN_CONFIG_FILE}" \
  --beacon-rpc-provider="${BEACON_RPC_PROVIDER}" \
  --beacon-rest-api-provider="${BEACON_REST_API_PROVIDER}" \
  --rpc \
  --grpc-gateway-host=0.0.0.0 \
  --grpc-gateway-port="${GATEWAY_PORT}"
`

func containerEnv(beaconGRPC, beaconREST string) []string {
	return []string{
		"WALLET_DIR=" + walletDir,
		"PASSWORD_FILE=" + passwordPath,
		"KEYS_DIR=" + keysDir,
		"CHAIN_CONFIG_FILE=" + chainConfigPath,
		"BEACON_RPC_PROVIDER=" + beaconGRPC,
		"BEACON_REST_API_PROVIDER=" + beaconREST,
		"GATEWAY_PORT=" + strconv.Itoa(gatewayPort),
	}
}

// Config describes the sidecar Start launches.
type Config struct {
	// Image is the Qrysm validator image, such as the devnet's own.
	Image string
	// BeaconURL and BeaconGRPC are the beacon node's host-published REST URL
	// and gRPC host:port.
	BeaconURL  string
	BeaconGRPC string
	// ConsensusServiceID names the Kurtosis consensus service whose chain
	// config the sidecar copies.
	ConsensusServiceID string
	// Keystores are imported at startup. They must be encrypted with
	// validatorops.KeystorePassword, which also protects the wallet.
	Keystores []sidecar.File
}

// Sidecar is a running validator client that the caller must Close.
type Sidecar struct {
	// Keymanager talks to the sidecar's keymanager API.
	Keymanager  *keymanager.Client
	container   *sidecar.Container
	beaconGRPC  string
	closeClient func() error
}

type dockerClient interface {
	sidecar.Client
	devnet.ServiceFileClient
}

// Start launches the sidecar and waits until its keymanager API answers.
func Start(ctx context.Context, config Config) (*Sidecar, error) {
	client, err := dockerapi.New()
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	validator, err := start(ctx, config, client)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	validator.closeClient = client.Close
	return validator, nil
}

func start(ctx context.Context, config Config, client dockerClient) (*Sidecar, error) {
	beaconREST, err := sidecar.HostURL(config.BeaconURL)
	if err != nil {
		return nil, fmt.Errorf("rewrite beacon URL: %w", err)
	}
	beaconGRPC, err := sidecar.HostAddress(config.BeaconGRPC)
	if err != nil {
		return nil, fmt.Errorf("rewrite beacon gRPC endpoint: %w", err)
	}
	chainConfig, err := readChainConfig(ctx, client, config.ConsensusServiceID)
	if err != nil {
		return nil, fmt.Errorf("copy chain config: %w", err)
	}
	files, err := fixtureFiles(chainConfig, config.Keystores)
	if err != nil {
		return nil, err
	}

	container, err := sidecar.Start(ctx, client, sidecar.Spec{
		Name:       sidecarName,
		Image:      config.Image,
		Entrypoint: []string{"/bin/sh", startScriptPath},
		Env:        containerEnv(beaconGRPC, beaconREST),
		Files:      files,
		Port:       gatewayPort,
	})
	if err != nil {
		return nil, err
	}
	keymanagerClient, err := waitForKeymanager(ctx, container)
	if err != nil {
		return nil, errors.Join(err, container.Close())
	}
	return &Sidecar{Keymanager: keymanagerClient, container: container, beaconGRPC: beaconGRPC}, nil
}

// Close removes the container and closes the Docker client.
func (validator *Sidecar) Close() error {
	if validator == nil {
		return nil
	}
	err := validator.container.Close()
	if validator.closeClient != nil {
		err = errors.Join(err, validator.closeClient())
	}
	return err
}

// VoluntaryExit signs and submits an exit for publicKey, a key in the wallet,
// through the validator binary's accounts command. That path dials the beacon
// itself and does not use the keymanager HTTP API.
//
// The accounts command ignores --chain-config-file and computes the exit epoch
// with mainnet timing, so on the devnet it signs an exit for an early epoch,
// which the beacon node accepts.
func (validator *Sidecar) VoluntaryExit(ctx context.Context, publicKey string) error {
	output, err := validator.container.Exec(ctx, "/validator",
		"accounts", "voluntary-exit",
		"--accept-terms-of-use",
		"--wallet-dir="+walletDir,
		"--wallet-password-file="+passwordPath,
		"--beacon-rpc-provider="+validator.beaconGRPC,
		"--public-keys="+publicKey,
		"--force-exit",
	)
	if err != nil {
		return err
	}
	// The accounts command exits 0 even when the beacon node rejects the exit.
	// It reports success once any selected key exits, so selecting one key
	// makes the success line mean that key.
	if !strings.Contains(output, "Voluntary exit was successful") {
		return fmt.Errorf("voluntary exit was not accepted: %s", strings.TrimSpace(output))
	}
	return nil
}

func fixtureFiles(chainConfig []byte, keystores []sidecar.File) ([]sidecar.File, error) {
	if len(chainConfig) == 0 {
		return nil, errors.New("chain config is empty")
	}
	files := []sidecar.File{
		{Name: startScriptPath, Body: []byte(startScript), Mode: 0o755},
		{Name: passwordPath, Body: []byte(walletPassword)},
		{Name: chainConfigPath, Body: chainConfig},
	}
	for _, keystore := range keystores {
		name := path.Base(strings.TrimSpace(keystore.Name))
		if name == "." || name == "/" {
			return nil, errors.New("keystore name is empty")
		}
		files = append(files, sidecar.File{Name: path.Join(keysDir, name), Body: keystore.Body})
	}
	return files, nil
}

func waitForKeymanager(ctx context.Context, container *sidecar.Container) (*keymanager.Client, error) {
	ticker := time.NewTicker(readyPollInterval)
	defer ticker.Stop()

	for {
		keymanagerClient, err := connectKeymanager(ctx, container)
		if err == nil {
			return keymanagerClient, nil
		}
		var exitErr *sidecar.ExitError
		if errors.As(err, &exitErr) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, container.WithLogs(fmt.Errorf("wait for %s: %w", sidecarName, errors.Join(err, context.Cause(ctx))))
		case <-ticker.C:
		}
	}
}

func connectKeymanager(ctx context.Context, container *sidecar.Container) (*keymanager.Client, error) {
	hostPort, err := container.PublishedPort(ctx)
	if err != nil {
		return nil, err
	}
	token, err := container.ReadFile(ctx, authTokenPath)
	if err != nil {
		return nil, fmt.Errorf("read validator auth token: %w", err)
	}
	parsed, err := parseAuthToken(token)
	if err != nil {
		return nil, err
	}
	keymanagerClient, err := keymanager.New("http://"+net.JoinHostPort("127.0.0.1", hostPort), parsed)
	if err != nil {
		return nil, err
	}
	if _, err := keymanagerClient.ListKeystores(ctx); err != nil {
		return nil, fmt.Errorf("query %s keymanager API: %w", sidecarName, err)
	}
	return keymanagerClient, nil
}

// parseAuthToken reads the token as Qrysm does: the last non-empty line.
func parseAuthToken(raw []byte) (string, error) {
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	token := strings.TrimSpace(lines[len(lines)-1])
	if token == "" {
		return "", errors.New("validator auth token is empty")
	}
	return token, nil
}

// readChainConfig copies the chain config out of the devnet's consensus client
// container.
func readChainConfig(ctx context.Context, client devnet.ServiceFileClient, serviceID string) ([]byte, error) {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return nil, errors.New("consensus service has no ID")
	}
	return devnet.ReadServiceFile(ctx, client, serviceID, chainConfigPath)
}
