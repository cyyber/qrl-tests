// Package dockerapi creates Docker Engine clients using the same connection
// settings as the Docker CLI.
package dockerapi

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	dockercontext "github.com/docker/cli/cli/context"
	dockerendpoint "github.com/docker/cli/cli/context/docker"
	dockercontextstore "github.com/docker/cli/cli/context/store"
	dockeropts "github.com/docker/cli/opts"
	dockerclient "github.com/moby/moby/client"
)

const (
	defaultContextName     = "default"
	dockerConfigEnv        = "DOCKER_CONFIG"
	dockerConfigFile       = "config.json"
	dockerContextEnv       = "DOCKER_CONTEXT"
	dockerCustomHeadersEnv = "DOCKER_CUSTOM_HEADERS"
	dockerTLSEnv           = "DOCKER_TLS"
)

type configFile struct {
	CurrentContext string            `json:"currentContext"`
	HTTPHeaders    map[string]string `json:"HttpHeaders"`
}

// New creates a Docker Engine client for the endpoint selected by the Docker
// environment, current CLI context, and configuration.
func New() (dockerclient.APIClient, error) {
	configDirectory, err := configDirectory()
	if err != nil {
		return nil, err
	}
	configuration := loadConfig(configDirectory)

	endpoint, err := resolveEndpoint(configDirectory, configuration.CurrentContext)
	if err != nil {
		return nil, fmt.Errorf("resolve Docker endpoint: %w", err)
	}
	options, err := endpoint.ClientOpts()
	if err != nil {
		return nil, fmt.Errorf("configure Docker endpoint: %w", err)
	}
	headerOptions, err := httpHeaderOptions(configuration.HTTPHeaders)
	if err != nil {
		return nil, err
	}
	options = append(options, headerOptions...)

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
	applyContextGoDebug(metadata)
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

func applyContextGoDebug(metadata dockercontextstore.Metadata) {
	if os.Getenv("GODEBUG") != "" {
		return
	}
	fields, ok := metadata.Metadata.(map[string]any)
	if !ok {
		return
	}
	value, ok := fields["GODEBUG"].(string)
	if ok {
		_ = os.Setenv("GODEBUG", value)
	}
}

func currentContext(configured string) string {
	if os.Getenv(dockerclient.EnvOverrideHost) != "" {
		return defaultContextName
	}
	if name := os.Getenv(dockerContextEnv); name != "" {
		return name
	}
	if configured != "" {
		return configured
	}
	return defaultContextName
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

func configDirectory() (string, error) {
	if directory := os.Getenv(dockerConfigEnv); directory != "" {
		return directory, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve Docker configuration directory: %w", err)
	}
	return filepath.Join(home, ".docker"), nil
}

func loadConfig(directory string) configFile {
	contents, err := os.ReadFile(filepath.Join(directory, dockerConfigFile))
	if errors.Is(err, os.ErrNotExist) {
		return configFile{}
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "WARNING: Error loading Docker configuration:", err)
		return configFile{}
	}
	var configuration configFile
	if err := json.Unmarshal(contents, &configuration); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "WARNING: Error loading Docker configuration:", err)
		return configFile{}
	}
	return configuration
}

func httpHeaderOptions(configured map[string]string) ([]dockerclient.Opt, error) {
	value := os.Getenv(dockerCustomHeadersEnv)
	if value == "" {
		if len(configured) == 0 {
			return nil, nil
		}
		return []dockerclient.Opt{dockerclient.WithHTTPHeaders(configured)}, nil
	}

	fields, err := csv.NewReader(strings.NewReader(value)).Read()
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", dockerCustomHeadersEnv, err)
	}
	headers := make(map[string]string, len(fields))
	for _, field := range fields {
		key, value, found := strings.Cut(field, "=")
		key = strings.TrimSpace(key)
		if key == "" || !found {
			return nil, fmt.Errorf("parse %s: invalid header %q", dockerCustomHeadersEnv, field)
		}
		headers[http.CanonicalHeaderKey(key)] = value
	}
	return []dockerclient.Opt{dockerclient.WithHTTPHeaders(headers)}, nil
}
