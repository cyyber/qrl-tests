package dockerapi

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cyyber/qrl-tests/internal/testutil"
	dockerendpoint "github.com/docker/cli/cli/context/docker"
	dockercontextstore "github.com/docker/cli/cli/context/store"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestNewUsesDockerConnectionPrecedence(t *testing.T) {
	configurationDirectory := dockerEnvironment(t)
	writeContext(t, configurationDirectory, "configured", "tcp://127.0.0.1:23751", nil)
	writeContext(t, configurationDirectory, "environment", "tcp://127.0.0.1:23752", nil)
	testutil.WriteJSON(
		t,
		filepath.Join(configurationDirectory, "config.json"),
		map[string]string{"currentContext": "configured"},
	)

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
	server := httptest.NewTLSServer(dockerHandler())
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
	server := httptest.NewTLSServer(dockerHandler())
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

func TestNewUsesAPIVersion(t *testing.T) {
	dockerEnvironment(t)
	t.Setenv(dockerclient.EnvOverrideAPIVersion, "1.44")

	client, err := New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	require.Equal(t, "1.44", client.ClientVersion())
}

func TestNewRejectsMissingContext(t *testing.T) {
	dockerEnvironment(t)
	t.Setenv(dockerContextEnv, "missing")
	_, err := New()
	require.ErrorContains(t, err, `load Docker context "missing"`)
}

func TestNewToleratesMalformedConfiguration(t *testing.T) {
	configurationDirectory := dockerEnvironment(t)
	require.NoError(t, os.WriteFile(
		filepath.Join(configurationDirectory, "config.json"),
		[]byte("{"),
		0o600,
	))

	client, err := New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	require.Equal(t, dockerclient.DefaultDockerHost, client.DaemonHost())
}

func TestConfigDirectoryFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Docker CLI does not use the current-user fallback on Windows")
	}
	t.Setenv(dockerConfigEnv, "")
	t.Setenv("HOME", "")
	if home, _ := os.UserHomeDir(); home != "" {
		t.Skip("platform provides a home directory without HOME")
	}
	current, err := user.Current()
	require.NoError(t, err)

	require.Equal(t, filepath.Join(current.HomeDir, ".docker"), configDirectory())
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
	return directory
}

func writeContext(
	t *testing.T,
	configurationDirectory string,
	name string,
	host string,
	tlsFiles map[string][]byte,
) {
	t.Helper()
	contextStore := dockercontextstore.New(
		filepath.Join(configurationDirectory, "contexts"),
		contextStoreConfig(),
	)
	require.NoError(t, contextStore.CreateOrUpdate(dockercontextstore.Metadata{
		Name: name,
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

func dockerHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"Version":"29.0.0","ApiVersion":"1.52","MinAPIVersion":"1.24"}`))
	})
}
