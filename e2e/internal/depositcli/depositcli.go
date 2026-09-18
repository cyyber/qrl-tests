// Package depositcli runs the Qrysm staking-deposit-cli against a live
// development network.
package depositcli

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/operatorvc"
	"github.com/cyyber/qrl-tests/internal/dockerapi"
	"github.com/moby/moby/api/pkg/stdcopy"
	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
)

const (
	// ImageEnv selects the locally built deposit CLI image.
	ImageEnv = "DEVNET_DEPOSIT_IMAGE"
	// DefaultImage is the local tag produced by `make deposit-image`.
	DefaultImage = "local/qrysm-deposit:devnet"

	chainName               = "dev"
	containerCleanupTimeout = 30 * time.Second
	startScriptPath         = "/run-deposit.sh"
	passwordPath            = "/keystore-password.txt"
	seedPath                = "/payer.seed"
	keysDir                 = "/keys"
)

// Result is the deposit CLI output the CLI suite asserts against.
type Result struct {
	PublicKey           string
	Amount              uint64
	WithdrawalRecipient string
	RandaoCommitment    string
	Keystores           []operatorvc.File
}

// Config runs one new-seed + submit cycle.
type Config struct {
	Image               string
	ExecutionRPCURL     string
	DepositContract     string
	WithdrawalRecipient string
	PayerSeed           string
	Password            string
}

// ImageFromEnv returns the configured deposit CLI image reference.
func ImageFromEnv() string {
	return cmp.Or(strings.TrimSpace(os.Getenv(ImageEnv)), DefaultImage)
}

// EnsureImage fails when the deposit CLI image is not present locally.
func EnsureImage(ctx context.Context, image string) error {
	image = strings.TrimSpace(image)
	if image == "" {
		return errors.New("deposit CLI image is empty")
	}
	client, err := dockerapi.New()
	if err != nil {
		return fmt.Errorf("create Docker client: %w", err)
	}
	defer func() { _ = client.Close() }()
	if _, err := client.ImageInspect(ctx, image); err != nil {
		return fmt.Errorf("deposit CLI image %q is not available (build it with make deposit-image): %w", image, err)
	}
	return nil
}

// Run generates one validator keystore and submits the compiled-in maximum
// deposit from the development wallet.
func Run(ctx context.Context, config Config) (Result, error) {
	client, err := dockerapi.New()
	if err != nil {
		return Result{}, fmt.Errorf("create Docker client: %w", err)
	}
	defer func() { _ = client.Close() }()
	return runWithClient(ctx, config, client)
}

type depositDockerClient interface {
	ContainerCreate(context.Context, dockerclient.ContainerCreateOptions) (dockerclient.ContainerCreateResult, error)
	ContainerLogs(context.Context, string, dockerclient.ContainerLogsOptions) (dockerclient.ContainerLogsResult, error)
	ContainerRemove(context.Context, string, dockerclient.ContainerRemoveOptions) (dockerclient.ContainerRemoveResult, error)
	ContainerStart(context.Context, string, dockerclient.ContainerStartOptions) (dockerclient.ContainerStartResult, error)
	ContainerWait(context.Context, string, dockerclient.ContainerWaitOptions) dockerclient.ContainerWaitResult
	CopyFromContainer(context.Context, string, dockerclient.CopyFromContainerOptions) (dockerclient.CopyFromContainerResult, error)
	CopyToContainer(context.Context, string, dockerclient.CopyToContainerOptions) (dockerclient.CopyToContainerResult, error)
	ImageInspect(context.Context, string, ...dockerclient.ImageInspectOption) (dockerclient.ImageInspectResult, error)
}

func runWithClient(ctx context.Context, config Config, client depositDockerClient) (Result, error) {
	if err := validateConfig(config); err != nil {
		return Result{}, err
	}
	if _, err := client.ImageInspect(ctx, config.Image); err != nil {
		return Result{}, fmt.Errorf("deposit CLI image %q is not available (build it with make deposit-image): %w", config.Image, err)
	}
	executionRPC, err := operatorvc.RewritePublishedURL(config.ExecutionRPCURL)
	if err != nil {
		return Result{}, fmt.Errorf("rewrite execution RPC URL: %w", err)
	}

	archive, err := depositFixtureArchive(config)
	if err != nil {
		return Result{}, err
	}

	created, err := client.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
		Config: &containertypes.Config{
			Image:      config.Image,
			Entrypoint: []string{"/bin/sh", startScriptPath},
			Env: []string{
				"DEPOSIT_EXECUTION_ADDRESS=" + config.WithdrawalRecipient,
				"DEPOSIT_EL_RPC=" + executionRPC,
				"DEPOSIT_CONTRACT=" + config.DepositContract,
			},
		},
		HostConfig: &containertypes.HostConfig{
			ExtraHosts: []string{"host.docker.internal:host-gateway"},
		},
	})
	if err != nil {
		return Result{}, fmt.Errorf("create deposit CLI container: %w", err)
	}
	if created.ID == "" {
		return Result{}, errors.New("create deposit CLI container: Docker returned no container ID")
	}

	remove := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), containerCleanupTimeout)
		defer cancel()
		_, _ = client.ContainerRemove(cleanupCtx, created.ID, dockerclient.ContainerRemoveOptions{Force: true})
	}
	defer remove()

	if _, err := client.CopyToContainer(ctx, created.ID, dockerclient.CopyToContainerOptions{
		DestinationPath: "/",
		Content:         bytes.NewReader(archive),
	}); err != nil {
		return Result{}, fmt.Errorf("copy deposit CLI fixtures: %w", err)
	}

	waiter := client.ContainerWait(ctx, created.ID, dockerclient.ContainerWaitOptions{
		Condition: containertypes.WaitConditionNextExit,
	})
	select {
	case err := <-waiter.Error:
		if err != nil {
			return Result{}, fmt.Errorf("register deposit CLI exit waiter: %w", err)
		}
	default:
	}

	if _, err := client.ContainerStart(ctx, created.ID, dockerclient.ContainerStartOptions{}); err != nil {
		return Result{}, fmt.Errorf("start deposit CLI container: %w", err)
	}

	var status containertypes.WaitResponse
	select {
	case err := <-waiter.Error:
		return Result{}, fmt.Errorf("wait for deposit CLI: %w", err)
	case status = <-waiter.Result:
	case <-ctx.Done():
		return Result{}, fmt.Errorf("wait for deposit CLI: %w", ctx.Err())
	}

	logs := containerLogs(ctx, client, created.ID)
	if status.StatusCode != 0 {
		return Result{}, fmt.Errorf("deposit CLI exited %d: %s", status.StatusCode, logs)
	}

	copied, err := client.CopyFromContainer(ctx, created.ID, dockerclient.CopyFromContainerOptions{
		SourcePath: keysDir,
	})
	if err != nil {
		return Result{}, fmt.Errorf("copy deposit CLI output: %w", err)
	}
	defer copied.Content.Close()
	files, err := readTarFiles(copied.Content)
	if err != nil {
		return Result{}, err
	}
	result, err := parseDepositOutput(files)
	if err != nil {
		return Result{}, err
	}
	return result, nil
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

func depositFixtureArchive(config Config) ([]byte, error) {
	return operatorvcTar(
		operatorvc.File{Name: strings.TrimPrefix(startScriptPath, "/"), Body: []byte(depositStartScript)},
		operatorvc.File{Name: strings.TrimPrefix(passwordPath, "/"), Body: []byte(config.Password)},
		operatorvc.File{Name: strings.TrimPrefix(seedPath, "/"), Body: []byte(strings.TrimSpace(config.PayerSeed))},
	)
}

// operatorvcTar is a tiny archive helper so deposit fixtures stay next to the
// CLI runner without exporting operatorvc's internal tar writer.
func operatorvcTar(files ...operatorvc.File) ([]byte, error) {
	return writeRegularFiles(files)
}

func containerLogs(ctx context.Context, client depositDockerClient, containerID string) string {
	logs, err := client.ContainerLogs(ctx, containerID, dockerclient.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return err.Error()
	}
	defer logs.Close()
	var output bytes.Buffer
	if _, err := stdcopy.StdCopy(&output, &output, logs); err != nil {
		return strings.TrimSpace(output.String() + "\n" + err.Error())
	}
	return strings.TrimSpace(output.String())
}

const depositStartScript = `#!/bin/sh
set -eu
/usr/local/bin/deposit new-seed \
  --num-validators=1 \
  --folder=/keys \
  --chain-name=dev \
  --execution-address="${DEPOSIT_EXECUTION_ADDRESS}" \
  --keystore-password-file=/keystore-password.txt \
  --lightkdf
/usr/local/bin/deposit submit \
  --validator-keys-dir=/keys \
  --seed-file=/payer.seed \
  --http-web3provider="${DEPOSIT_EL_RPC}" \
  --deposit-contract="${DEPOSIT_CONTRACT}" \
  --skip-deposit-confirmation \
  --deposit-delay-seconds=0
`
