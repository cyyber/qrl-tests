package staker

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
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
	operatorContainerHost           = "host.docker.internal"
	operatorContainerCleanupTimeout = 30 * time.Second
	operatorReadyPollInterval       = 500 * time.Millisecond
	operatorGatewayPort             = 7500
	operatorWalletDir               = "/wallet"
	operatorPasswordPath            = "/wallet-password.txt"
	operatorConfigPath              = "/network-configs/config.yaml"
	operatorStartScriptPath         = "/start-validator.sh"
	operatorAuthTokenPath           = operatorWalletDir + "/auth-token"
	kurtosisServiceUUIDDockerLabel  = "com.kurtosistech.guid"
)

const operatorStartScript = `#!/bin/sh
set -eu
if [ ! -d /wallet/direct ]; then
  /validator wallet create \
    --accept-terms-of-use \
    --wallet-dir=/wallet \
    --wallet-password-file=/wallet-password.txt \
    --keymanager-kind=imported
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

type operatorValidatorConfig struct {
	image              string
	beaconHTTPURL      string
	beaconGRPC         string
	consensusServiceID string
}

type operatorValidator struct {
	Client *validatorclient.Client
	close  func(context.Context) error
}

func (operator *operatorValidator) Close() error {
	if operator == nil || operator.close == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), operatorContainerCleanupTimeout)
	defer cancel()
	return operator.close(ctx)
}

type operatorDockerClient interface {
	ContainerCreate(context.Context, dockerclient.ContainerCreateOptions) (dockerclient.ContainerCreateResult, error)
	ContainerInspect(context.Context, string, dockerclient.ContainerInspectOptions) (dockerclient.ContainerInspectResult, error)
	ContainerList(context.Context, dockerclient.ContainerListOptions) (dockerclient.ContainerListResult, error)
	ContainerRemove(context.Context, string, dockerclient.ContainerRemoveOptions) (dockerclient.ContainerRemoveResult, error)
	ContainerStart(context.Context, string, dockerclient.ContainerStartOptions) (dockerclient.ContainerStartResult, error)
	CopyFromContainer(context.Context, string, dockerclient.CopyFromContainerOptions) (dockerclient.CopyFromContainerResult, error)
	CopyToContainer(context.Context, string, dockerclient.CopyToContainerOptions) (dockerclient.CopyToContainerResult, error)
}

func startOperatorValidator(ctx context.Context, config operatorValidatorConfig) (*operatorValidator, error) {
	client, err := dockerapi.New()
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	operator, err := startOperatorValidatorWithClient(ctx, config, client)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	closeOperator := operator.close
	operator.close = func(ctx context.Context) error {
		return errors.Join(closeOperator(ctx), client.Close())
	}
	return operator, nil
}

func startOperatorValidatorWithClient(
	ctx context.Context,
	config operatorValidatorConfig,
	client operatorDockerClient,
) (*operatorValidator, error) {
	beaconREST, err := rewritePublishedURL(config.beaconHTTPURL)
	if err != nil {
		return nil, fmt.Errorf("rewrite beacon HTTP URL: %w", err)
	}
	beaconGRPC, err := rewritePublishedHost(config.beaconGRPC)
	if err != nil {
		return nil, fmt.Errorf("rewrite beacon gRPC endpoint: %w", err)
	}
	genesisConfig, err := copyServiceFile(ctx, client, config.consensusServiceID, operatorConfigPath)
	if err != nil {
		return nil, fmt.Errorf("copy genesis config: %w", err)
	}

	archive, err := operatorFixtureArchive(genesisConfig)
	if err != nil {
		return nil, err
	}

	created, err := client.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
		Config: &containertypes.Config{
			Image:      config.image,
			Entrypoint: []string{"/bin/sh", operatorStartScriptPath},
			Env: []string{
				"BEACON_RPC_PROVIDER=" + beaconGRPC,
				"BEACON_REST_API_PROVIDER=" + beaconREST,
			},
			ExposedPorts: network.PortSet{operatorGatewayNetworkPort(): {}},
		},
		HostConfig: &containertypes.HostConfig{
			ExtraHosts: []string{operatorContainerHost + ":host-gateway"},
			PortBindings: network.PortMap{
				operatorGatewayNetworkPort(): {{
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

	validatorClient, err := waitForOperatorValidator(ctx, client, created.ID)
	if err != nil {
		_ = remove(context.Background())
		return nil, err
	}

	return &operatorValidator{
		Client: validatorClient,
		close:  remove,
	}, nil
}

func waitForOperatorValidator(
	ctx context.Context,
	client operatorDockerClient,
	containerID string,
) (*validatorclient.Client, error) {
	ticker := time.NewTicker(operatorReadyPollInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		validatorClient, err := inspectOperatorValidator(ctx, client, containerID)
		if err == nil {
			return validatorClient, nil
		}
		var exitErr *operatorValidatorExitError
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

func inspectOperatorValidator(
	ctx context.Context,
	client operatorDockerClient,
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
		return nil, &operatorValidatorExitError{status: status}
	}
	hostPort, err := publishedHostPort(inspected.Container, operatorGatewayPort)
	if err != nil {
		return nil, err
	}
	token, err := copyContainerFile(ctx, client, containerID, operatorAuthTokenPath)
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
	client operatorDockerClient,
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
	client operatorDockerClient,
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

func operatorFixtureArchive(genesisConfig []byte) ([]byte, error) {
	if len(genesisConfig) == 0 {
		return nil, errors.New("genesis config is empty")
	}
	return tarFiles([]tarFile{
		{Name: strings.TrimPrefix(operatorStartScriptPath, "/"), Mode: 0o755, Body: []byte(operatorStartScript)},
		{Name: strings.TrimPrefix(operatorPasswordPath, "/"), Mode: 0o600, Body: []byte(validatorops.KeystorePassword)},
		{Name: "network-configs", Mode: 0o755, Directory: true},
		{Name: strings.TrimPrefix(operatorConfigPath, "/"), Mode: 0o600, Body: genesisConfig},
	})
}

type tarFile struct {
	Name      string
	Mode      int64
	Body      []byte
	Directory bool
}

func tarFiles(files []tarFile) ([]byte, error) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, file := range files {
		header := &tar.Header{Name: file.Name, Mode: file.Mode}
		if file.Directory {
			header.Typeflag = tar.TypeDir
		} else {
			header.Typeflag = tar.TypeReg
			header.Size = int64(len(file.Body))
		}
		if err := writer.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("archive %s: %w", file.Name, err)
		}
		if file.Directory {
			continue
		}
		if _, err := writer.Write(file.Body); err != nil {
			return nil, fmt.Errorf("archive %s: %w", file.Name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("archive operator validator fixtures: %w", err)
	}
	return archive.Bytes(), nil
}

func readTarFile(reader io.Reader, want string) ([]byte, error) {
	archive := tar.NewReader(reader)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("archive does not contain %s", want)
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if path.Base(header.Name) != want {
			continue
		}
		return io.ReadAll(archive)
	}
}

func parseAuthToken(raw []byte) (string, error) {
	var token string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		token = line
	}
	if token == "" {
		return "", errors.New("validator auth token is empty")
	}
	return token, nil
}

func rewritePublishedURL(endpointURL string) (string, error) {
	endpoint, err := url.Parse(endpointURL)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	if endpoint.Scheme == "" || endpoint.Hostname() == "" || endpoint.Port() == "" {
		return "", errors.New("URL must include a scheme, host, and port")
	}
	endpoint.Host = net.JoinHostPort(operatorContainerHost, endpoint.Port())
	return endpoint.String(), nil
}

func rewritePublishedHost(address string) (string, error) {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("parse host:port: %w", err)
	}
	if port == "" {
		return "", errors.New("address must include a port")
	}
	return net.JoinHostPort(operatorContainerHost, port), nil
}

func publishedHostPort(inspected containertypes.InspectResponse, containerPort uint16) (string, error) {
	if inspected.NetworkSettings == nil {
		return "", errors.New("container has no network settings")
	}
	port, ok := network.PortFrom(containerPort, network.TCP)
	if !ok {
		return "", fmt.Errorf("invalid container port %d", containerPort)
	}
	bindings := inspected.NetworkSettings.Ports[port]
	if len(bindings) == 0 || bindings[0].HostPort == "" {
		return "", fmt.Errorf("container port %d is not published", containerPort)
	}
	return bindings[0].HostPort, nil
}

type operatorValidatorExitError struct {
	status string
}

func (err *operatorValidatorExitError) Error() string {
	return "operator validator container is " + err.status
}

func operatorGatewayNetworkPort() network.Port {
	port, ok := network.PortFrom(operatorGatewayPort, network.TCP)
	if !ok {
		panic("operator gateway port is invalid")
	}
	return port
}
