// Package operatorvc launches the dedicated validator-client sidecar used by
// the consensus E2E suites.
package operatorvc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"path"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/validatorclient"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
	"github.com/cyyber/qrl-tests/internal/dockerapi"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	dockerclient "github.com/moby/moby/client"
)

const (
	containerCleanupTimeout        = 30 * time.Second
	readyPollInterval              = 500 * time.Millisecond
	gatewayPort                    = 7500
	walletDir                      = "/wallet"
	passwordPath                   = "/wallet-password.txt"
	configPath                     = "/network-configs/config.yaml"
	startScriptPath                = "/start-validator.sh"
	authTokenPath                  = walletDir + "/auth-token"
	walletPassword                 = validatorops.KeystorePassword
	kurtosisServiceUUIDDockerLabel = "com.kurtosistech.guid"
)

const startScript = `#!/bin/sh
set -eu
if [ ! -d /wallet/direct ]; then
  /validator wallet create \
    --accept-terms-of-use \
    --wallet-dir=/wallet \
    --wallet-password-file=/wallet-password.txt \
    --keymanager-kind=imported
fi
if [ -d /keys ]; then
  /validator accounts import \
    --accept-terms-of-use \
    --wallet-dir=/wallet \
    --wallet-password-file=/wallet-password.txt \
    --keys-dir=/keys \
    --account-password-file=/wallet-password.txt
fi
exec /validator \
  --accept-terms-of-use \
  --wallet-dir=/wallet \
  --wallet-password-file=/wallet-password.txt \
  --chain-config-file=/network-configs/config.yaml \
  --beacon-rpc-provider="${BEACON_RPC_PROVIDER}" \
  --beacon-rest-api-provider="${BEACON_REST_API_PROVIDER}" \
  --rpc \
  --rpc-host=0.0.0.0 \
  --rpc-port=7000 \
  --grpc-gateway-host=0.0.0.0 \
  --grpc-gateway-port=7500 \
  --monitoring-host=0.0.0.0 \
  --monitoring-port=8081
`

// Config starts one operator validator sidecar.
type Config struct {
	Image              string
	BeaconHTTPURL      string
	BeaconGRPC         string
	ConsensusServiceID string
	Keystores          []File
}

// Validator is a running operator validator sidecar.
type Validator struct {
	Keymanager *validatorclient.Client
	beaconGRPC string
	id         string
	exec       execClient
	close      func(context.Context) error
}

type dockerClient interface {
	ContainerCreate(context.Context, dockerclient.ContainerCreateOptions) (dockerclient.ContainerCreateResult, error)
	ContainerInspect(context.Context, string, dockerclient.ContainerInspectOptions) (dockerclient.ContainerInspectResult, error)
	ContainerList(context.Context, dockerclient.ContainerListOptions) (dockerclient.ContainerListResult, error)
	ContainerRemove(context.Context, string, dockerclient.ContainerRemoveOptions) (dockerclient.ContainerRemoveResult, error)
	ContainerStart(context.Context, string, dockerclient.ContainerStartOptions) (dockerclient.ContainerStartResult, error)
	CopyFromContainer(context.Context, string, dockerclient.CopyFromContainerOptions) (dockerclient.CopyFromContainerResult, error)
	CopyToContainer(context.Context, string, dockerclient.CopyToContainerOptions) (dockerclient.CopyToContainerResult, error)
}

type execClient interface {
	ExecCreate(context.Context, string, dockerclient.ExecCreateOptions) (dockerclient.ExecCreateResult, error)
	ExecAttach(context.Context, string, dockerclient.ExecAttachOptions) (dockerclient.ExecAttachResult, error)
	ExecInspect(context.Context, string, dockerclient.ExecInspectOptions) (dockerclient.ExecInspectResult, error)
}

// Close removes the sidecar container.
func (validator *Validator) Close() error {
	if validator == nil || validator.close == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), containerCleanupTimeout)
	defer cancel()
	return validator.close(ctx)
}

// Start creates the sidecar, copies the genesis config, optionally imports
// keystores through the validator binary, and waits until the keymanager API
// answers.
func Start(ctx context.Context, config Config) (*Validator, error) {
	client, err := dockerapi.New()
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	validator, err := startWithClient(ctx, config, client, client)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	closeValidator := validator.close
	validator.close = func(ctx context.Context) error {
		return errors.Join(closeValidator(ctx), client.Close())
	}
	return validator, nil
}

func startWithClient(ctx context.Context, config Config, client dockerClient, exec execClient) (*Validator, error) {
	beaconREST, err := RewritePublishedURL(config.BeaconHTTPURL)
	if err != nil {
		return nil, fmt.Errorf("rewrite beacon HTTP URL: %w", err)
	}
	beaconGRPC, err := rewritePublishedHost(config.BeaconGRPC)
	if err != nil {
		return nil, fmt.Errorf("rewrite beacon gRPC endpoint: %w", err)
	}
	genesisConfig, err := copyServiceFile(ctx, client, config.ConsensusServiceID, configPath)
	if err != nil {
		return nil, fmt.Errorf("copy genesis config: %w", err)
	}

	archive, err := fixtureArchive(genesisConfig, config.Keystores)
	if err != nil {
		return nil, err
	}

	created, err := client.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
		Config: &containertypes.Config{
			Image:      config.Image,
			Entrypoint: []string{"/bin/sh", startScriptPath},
			Env: []string{
				"BEACON_RPC_PROVIDER=" + beaconGRPC,
				"BEACON_REST_API_PROVIDER=" + beaconREST,
			},
			ExposedPorts: network.PortSet{gatewayNetworkPort(): {}},
		},
		HostConfig: &containertypes.HostConfig{
			ExtraHosts: []string{containerHost + ":host-gateway"},
			PortBindings: network.PortMap{
				gatewayNetworkPort(): {{
					HostIP: netip.MustParseAddr("127.0.0.1"),
				}},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create operator validator container: %w", err)
	}
	if created.ID == "" {
		return nil, errors.New("create operator validator container: Docker returned no container ID")
	}

	remove := func(cleanupCtx context.Context) error {
		if _, err := client.ContainerRemove(cleanupCtx, created.ID, dockerclient.ContainerRemoveOptions{Force: true}); err != nil {
			return fmt.Errorf("remove operator validator container: %w", err)
		}
		return nil
	}

	if _, err := client.CopyToContainer(ctx, created.ID, dockerclient.CopyToContainerOptions{
		DestinationPath: "/",
		Content:         bytes.NewReader(archive),
	}); err != nil {
		_ = remove(context.Background())
		return nil, fmt.Errorf("copy operator validator fixtures: %w", err)
	}

	if _, err := client.ContainerStart(ctx, created.ID, dockerclient.ContainerStartOptions{}); err != nil {
		_ = remove(context.Background())
		return nil, fmt.Errorf("start operator validator container: %w", err)
	}

	validatorClient, err := waitForValidator(ctx, client, created.ID)
	if err != nil {
		_ = remove(context.Background())
		return nil, err
	}

	return &Validator{
		Keymanager: validatorClient,
		beaconGRPC: beaconGRPC,
		id:         created.ID,
		exec:       exec,
		close:      remove,
	}, nil
}

func waitForValidator(
	ctx context.Context,
	client dockerClient,
	containerID string,
) (*validatorclient.Client, error) {
	ticker := time.NewTicker(readyPollInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		validatorClient, err := inspectValidator(ctx, client, containerID)
		if err == nil {
			return validatorClient, nil
		}
		var exitErr *containerExitError
		if errors.As(err, &exitErr) {
			return nil, err
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for operator validator: %w", errors.Join(lastErr, ctx.Err()))
		case <-ticker.C:
		}
	}
}

func inspectValidator(
	ctx context.Context,
	client dockerClient,
	containerID string,
) (*validatorclient.Client, error) {
	inspected, err := client.ContainerInspect(ctx, containerID, dockerclient.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("inspect operator validator container: %w", err)
	}
	if inspected.Container.State != nil && !inspected.Container.State.Running {
		status := string(inspected.Container.State.Status)
		if inspected.Container.State.Error != "" {
			status += ": " + inspected.Container.State.Error
		}
		return nil, &containerExitError{status: status}
	}
	hostPort, err := publishedHostPort(inspected.Container, gatewayPort)
	if err != nil {
		return nil, err
	}
	token, err := copyContainerFile(ctx, client, containerID, authTokenPath)
	if err != nil {
		return nil, fmt.Errorf("read validator auth token: %w", err)
	}
	parsed, err := parseAuthToken(token)
	if err != nil {
		return nil, err
	}
	validatorClient, err := validatorclient.New("http://"+net.JoinHostPort("127.0.0.1", hostPort), parsed)
	if err != nil {
		return nil, err
	}
	if _, err := validatorClient.ListKeystores(ctx); err != nil {
		return nil, fmt.Errorf("query operator validator API: %w", err)
	}
	return validatorClient, nil
}

func copyServiceFile(
	ctx context.Context,
	client dockerClient,
	serviceID, sourcePath string,
) ([]byte, error) {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return nil, errors.New("consensus service has no ID")
	}
	containers, err := client.ContainerList(ctx, dockerclient.ContainerListOptions{
		Filters: make(dockerclient.Filters).Add(
			"label",
			kurtosisServiceUUIDDockerLabel+"="+serviceID,
		),
	})
	if err != nil {
		return nil, fmt.Errorf("find consensus container: %w", err)
	}
	if len(containers.Items) != 1 {
		return nil, fmt.Errorf(
			"expected one running Docker container for service %q, found %d",
			serviceID,
			len(containers.Items),
		)
	}
	return copyContainerFile(ctx, client, containers.Items[0].ID, sourcePath)
}

func copyContainerFile(
	ctx context.Context,
	client dockerClient,
	containerID, sourcePath string,
) ([]byte, error) {
	copied, err := client.CopyFromContainer(ctx, containerID, dockerclient.CopyFromContainerOptions{
		SourcePath: sourcePath,
	})
	if err != nil {
		return nil, err
	}
	defer copied.Content.Close()
	return readTarFile(copied.Content, path.Base(sourcePath))
}

type containerExitError struct {
	status string
}

func (err *containerExitError) Error() string {
	return "operator validator container is " + err.status
}
