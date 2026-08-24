package dockerapi

import (
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dockerendpoint "github.com/docker/cli/cli/context/docker"
	dockercontextstore "github.com/docker/cli/cli/context/store"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestNewUsesDockerConnectionPrecedence(t *testing.T) {
	configurationDirectory := dockerEnvironment(t)
	writeContext(t, configurationDirectory, "configured", "tcp://127.0.0.1:23751", nil)
	writeContext(t, configurationDirectory, "environment", "tcp://127.0.0.1:23752", nil)
	writeConfig(t, configurationDirectory, `{"currentContext":"configured"}`)

	client, err := New()
	require.NoError(t, err)
	require.Equal(t, "tcp://127.0.0.1:23751", client.DaemonHost())
	require.NoError(t, client.Close())

	t.Setenv(dockerContextEnv, "environment")
	client, err = New()
	require.NoError(t, err)
	require.Equal(t, "tcp://127.0.0.1:23752", client.DaemonHost())
	require.NoError(t, client.Close())

	t.Setenv(dockerclient.EnvOverrideHost, "tcp://127.0.0.1:23753")
	client, err = New()
	require.NoError(t, err)
	require.Equal(t, "tcp://127.0.0.1:23753", client.DaemonHost())
	require.NoError(t, client.Close())
}

func TestNewUsesPlatformDefaultEndpoint(t *testing.T) {
	dockerEnvironment(t)
	client, err := New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	require.Equal(t, dockerclient.DefaultDockerHost, client.DaemonHost())
}

func TestNewUsesNamedContextTLS(t *testing.T) {
	configurationDirectory := dockerEnvironment(t)
	server := dockerTLSServer(t, nil)
	defer server.Close()

	certificate := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: server.Certificate().Raw,
	})
	writeContext(
		t,
		configurationDirectory,
		"remote",
		"tcp://"+server.Listener.Addr().String(),
		map[string][]byte{"ca.pem": certificate},
	)
	t.Setenv(dockerContextEnv, "remote")

	client, err := New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	version, err := client.ServerVersion(t.Context(), dockerclient.ServerVersionOptions{})
	require.NoError(t, err)
	require.Equal(t, "29.0.0", version.Version)
}

func TestNewUsesDefaultContextTLS(t *testing.T) {
	configurationDirectory := dockerEnvironment(t)
	server := dockerTLSServer(t, nil)
	defer server.Close()

	require.NoError(t, os.WriteFile(
		filepath.Join(configurationDirectory, "ca.pem"),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}),
		0o600,
	))
	t.Setenv(dockerclient.EnvOverrideHost, "tcp://"+server.Listener.Addr().String())
	t.Setenv(dockerclient.EnvTLSVerify, "1")

	client, err := New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	_, err = client.ServerVersion(t.Context(), dockerclient.ServerVersionOptions{})
	require.NoError(t, err)
}

func TestNewConfiguresSSHContext(t *testing.T) {
	configurationDirectory := dockerEnvironment(t)
	writeContext(t, configurationDirectory, "remote", "ssh://user@example.test", nil)
	t.Setenv(dockerContextEnv, "remote")

	client, err := New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	require.NotNil(t, client.Dialer())
}

func TestNewAppliesNamedContextGoDebug(t *testing.T) {
	configurationDirectory := dockerEnvironment(t)
	t.Setenv("GODEBUG", "")
	writeContext(
		t,
		configurationDirectory,
		"remote",
		"tcp://127.0.0.1:23751",
		nil,
		map[string]any{"GODEBUG": "dockerapi-test=1"},
	)
	t.Setenv(dockerContextEnv, "remote")

	client, err := New()
	require.NoError(t, err)
	require.NoError(t, client.Close())
	require.Equal(t, "dockerapi-test=1", os.Getenv("GODEBUG"))

	t.Setenv("GODEBUG", "user-setting=1")
	client, err = New()
	require.NoError(t, err)
	require.NoError(t, client.Close())
	require.Equal(t, "user-setting=1", os.Getenv("GODEBUG"))
}

func TestNewUsesAPIversionAndCustomHeaders(t *testing.T) {
	configurationDirectory := dockerEnvironment(t)
	requests := make(chan http.Header, 1)
	server := dockerServer(t, func(request *http.Request) {
		requests <- request.Header.Clone()
	})
	defer server.Close()

	writeConfig(t, configurationDirectory, `{"HttpHeaders":{"X-Configured":"configured"}}`)
	t.Setenv(dockerclient.EnvOverrideHost, strings.Replace(server.URL, "http://", "tcp://", 1))
	t.Setenv(dockerclient.EnvOverrideAPIVersion, "1.44")

	client, err := New()
	require.NoError(t, err)
	require.Equal(t, "1.44", client.ClientVersion())
	_, err = client.ServerVersion(t.Context(), dockerclient.ServerVersionOptions{})
	require.NoError(t, err)
	require.Equal(t, "configured", (<-requests).Get("X-Configured"))
	require.NoError(t, client.Close())

	t.Setenv(dockerCustomHeadersEnv, `x-environment=one,"x-comma=two,three"`)
	client, err = New()
	require.NoError(t, err)
	_, err = client.ServerVersion(t.Context(), dockerclient.ServerVersionOptions{})
	require.NoError(t, err)
	headers := <-requests
	require.Empty(t, headers.Get("X-Configured"))
	require.Equal(t, "one", headers.Get("X-Environment"))
	require.Equal(t, "two,three", headers.Get("X-Comma"))
	require.NoError(t, client.Close())
}

func TestNewRejectsInvalidCustomHeaders(t *testing.T) {
	dockerEnvironment(t)
	t.Setenv(dockerCustomHeadersEnv, "missing-equals")
	_, err := New()
	require.ErrorContains(t, err, "invalid header")
}

func TestNewRejectsMissingContext(t *testing.T) {
	dockerEnvironment(t)
	t.Setenv(dockerContextEnv, "missing")
	_, err := New()
	require.ErrorContains(t, err, `load Docker context "missing"`)
}

func TestNewToleratesMalformedConfiguration(t *testing.T) {
	configurationDirectory := dockerEnvironment(t)
	writeConfig(t, configurationDirectory, `{`)

	client, err := New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	require.Equal(t, dockerclient.DefaultDockerHost, client.DaemonHost())
}

func dockerEnvironment(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	t.Setenv(dockerConfigEnv, directory)
	t.Setenv(dockerclient.EnvOverrideHost, "")
	t.Setenv(dockerContextEnv, "")
	t.Setenv(dockerTLSEnv, "")
	t.Setenv(dockerclient.EnvTLSVerify, "")
	t.Setenv(dockerclient.EnvOverrideCertPath, "")
	t.Setenv(dockerclient.EnvOverrideAPIVersion, "")
	t.Setenv(dockerCustomHeadersEnv, "")
	return directory
}

func writeConfig(t *testing.T, directory, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "config.json"), []byte(contents), 0o600))
}

func writeContext(
	t *testing.T,
	configurationDirectory string,
	name string,
	host string,
	tlsFiles map[string][]byte,
	metadata ...map[string]any,
) {
	t.Helper()
	var contextMetadata any
	if len(metadata) > 0 {
		contextMetadata = metadata[0]
	}
	contextStore := dockercontextstore.New(
		filepath.Join(configurationDirectory, "contexts"),
		contextStoreConfig(),
	)
	require.NoError(t, contextStore.CreateOrUpdate(dockercontextstore.Metadata{
		Name:     name,
		Metadata: contextMetadata,
		Endpoints: map[string]any{
			dockerendpoint.DockerEndpoint: dockerendpoint.EndpointMeta{Host: host},
		},
	}))
	if tlsFiles != nil {
		require.NoError(t, contextStore.ResetEndpointTLSMaterial(
			name,
			dockerendpoint.DockerEndpoint,
			&dockercontextstore.EndpointTLSData{Files: tlsFiles},
		))
	}
}

func dockerServer(t *testing.T, inspect func(*http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if inspect != nil {
			inspect(request)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, err := writer.Write([]byte(`{"Version":"29.0.0","ApiVersion":"1.52","MinAPIVersion":"1.24"}`))
		require.NoError(t, err)
	}))
}

func dockerTLSServer(t *testing.T, inspect func(*http.Request)) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if inspect != nil {
			inspect(request)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, err := writer.Write([]byte(`{"Version":"29.0.0","ApiVersion":"1.52","MinAPIVersion":"1.24"}`))
		require.NoError(t, err)
	}))
	server.StartTLS()
	require.NotNil(t, server.Certificate())
	require.NotNil(t, server.Certificate().Raw)
	_, err := x509.ParseCertificate(server.Certificate().Raw)
	require.NoError(t, err)
	return server
}
