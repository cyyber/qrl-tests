// Package dockerapi creates Docker Engine API clients from Docker environment
// variables and CLI context configuration.
package dockerapi

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"

	"github.com/cyyber/qrl-tests/internal/jsonfile"
	dockercontext "github.com/docker/cli/cli/context"
	dockerendpoint "github.com/docker/cli/cli/context/docker"
	dockercontextstore "github.com/docker/cli/cli/context/store"
	dockeropts "github.com/docker/cli/opts"
	dockerclient "github.com/moby/moby/client"
)

const (
	defaultContextName = "default"
	dockerConfigEnv    = "DOCKER_CONFIG"
	dockerConfigFile   = "config.json"
	dockerContextEnv   = "DOCKER_CONTEXT"
	dockerTLSEnv       = "DOCKER_TLS"
)

// connectionConfig contains the Docker CLI setting used to select an API endpoint.
type connectionConfig struct {
	CurrentContext string `json:"currentContext"`
}

// New creates a Docker Engine API client for the endpoint selected by the Docker
// environment, current CLI context, and configuration.
// The caller must close the returned client.
func New() (dockerclient.APIClient, error) {
	configDirectory := configDirectory()
	configuration := loadConnectionConfig(configDirectory)

	endpoint, err := resolveEndpoint(configDirectory, configuration.CurrentContext)
	if err != nil {
		return nil, fmt.Errorf("resolve Docker endpoint: %w", err)
	}
	options, err := endpoint.ClientOpts()
	if err != nil {
		return nil, fmt.Errorf("configure Docker endpoint: %w", err)
	}

	client, err := dockerclient.New(options...)
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	return client, nil
}

func resolveEndpoint(configDirectory, configuredContext string) (dockerendpoint.Endpoint, error) {
	contextName := currentContext(configuredContext)
	if contextName == defaultContextName {
		return defaultEndpoint(configDirectory)
	}

	contextStore := dockercontextstore.New(
		filepath.Join(configDirectory, "contexts"),
		contextStoreConfig(),
	)
	metadata, err := contextStore.GetMetadata(contextName)
	if err != nil {
		return dockerendpoint.Endpoint{}, fmt.Errorf("load Docker context %q: %w", contextName, err)
	}
	endpointMetadata, err := dockerendpoint.EndpointFromContext(metadata)
	if err != nil {
		return dockerendpoint.Endpoint{}, fmt.Errorf("load Docker context %q endpoint: %w", contextName, err)
	}
	endpoint, err := dockerendpoint.WithTLSData(contextStore, contextName, endpointMetadata)
	if err != nil {
		return dockerendpoint.Endpoint{}, fmt.Errorf("load Docker context %q TLS data: %w", contextName, err)
	}
	return endpoint, nil
}

func currentContext(configured string) string {
	if os.Getenv(dockerclient.EnvOverrideHost) != "" {
		return defaultContextName
	}
	return cmp.Or(os.Getenv(dockerContextEnv), configured, defaultContextName)
}

func defaultEndpoint(configDirectory string) (dockerendpoint.Endpoint, error) {
	tlsEnabled := os.Getenv(dockerTLSEnv) != "" || os.Getenv(dockerclient.EnvTLSVerify) != ""
	host, err := dockeropts.ParseHost(tlsEnabled, os.Getenv(dockerclient.EnvOverrideHost))
	if err != nil {
		return dockerendpoint.Endpoint{}, err
	}
	endpoint := dockerendpoint.Endpoint{EndpointMeta: dockerendpoint.EndpointMeta{
		Host:          host,
		SkipTLSVerify: tlsEnabled && os.Getenv(dockerclient.EnvTLSVerify) == "",
	}}
	if !tlsEnabled {
		return endpoint, nil
	}

	certificateDirectory := os.Getenv(dockerclient.EnvOverrideCertPath)
	if certificateDirectory == "" {
		certificateDirectory = configDirectory
	}
	certificatePath := optionalFile(filepath.Join(certificateDirectory, "cert.pem"))
	keyPath := optionalFile(filepath.Join(certificateDirectory, "key.pem"))
	endpoint.TLSData, err = dockercontext.TLSDataFromFiles(
		filepath.Join(certificateDirectory, "ca.pem"),
		certificatePath,
		keyPath,
	)
	return endpoint, err
}

func optionalFile(path string) string {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return ""
	}
	return path
}

func contextStoreConfig() dockercontextstore.Config {
	return dockercontextstore.NewConfig(
		nil,
		dockercontextstore.EndpointTypeGetter(
			dockerendpoint.DockerEndpoint,
			func() any { return &dockerendpoint.EndpointMeta{} },
		),
	)
}

func configDirectory() string {
	if directory := os.Getenv(dockerConfigEnv); directory != "" {
		return directory
	}
	home, _ := os.UserHomeDir()
	if home == "" && runtime.GOOS != "windows" {
		if current, err := user.Current(); err == nil {
			home = current.HomeDir
		}
	}
	return filepath.Join(home, ".docker")
}

func loadConnectionConfig(directory string) connectionConfig {
	configPath := filepath.Join(directory, dockerConfigFile)
	configuration, err := jsonfile.Read[connectionConfig](configPath, "Docker configuration")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return connectionConfig{}
		}
		_, _ = fmt.Fprintf(os.Stderr, "WARNING: %v\n", err)
		return connectionConfig{}
	}
	return configuration
}
