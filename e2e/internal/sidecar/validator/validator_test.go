package validator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/sidecartest"
	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ dockerClient = (*sidecartest.Docker)(nil)

func TestStartScriptVariablesAreSet(t *testing.T) {
	set := map[string]bool{}
	for _, variable := range containerEnv("beacon:4000", "http://beacon:3500") {
		name, _, _ := strings.Cut(variable, "=")
		set[name] = true
	}
	for _, match := range regexp.MustCompile(`\$\{([A-Z_]+)\}`).FindAllStringSubmatch(startScript, -1) {
		require.True(t, set[match[1]], "start script reads %s, which containerEnv does not set", match[1])
	}
}

func TestStartWaitsForKeymanager(t *testing.T) {
	validator, docker := startWithKeymanager(t)
	require.NotNil(t, validator.Keymanager)
	require.Contains(t, docker.Created.Config.Env, "BEACON_REST_API_PROVIDER=http://host.docker.internal:3500")
	require.Contains(t, docker.Created.Config.Env, "BEACON_RPC_PROVIDER=host.docker.internal:4000")
	require.Len(t, docker.Listed, 1)
	require.Equal(t, dockerclient.Filters{"label": {"com.kurtosistech.guid=consensus-service": true}}, docker.Listed[0].Filters)

	require.NoError(t, validator.Close())
	require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
}

func TestStartFailures(t *testing.T) {
	for _, test := range []struct {
		name     string
		state    *containertypes.State
		logs     string
		cancel   error
		wantErrs []string
	}{
		{
			name:     "sidecar exits",
			state:    &containertypes.State{Status: containertypes.StateExited, ExitCode: 1},
			logs:     "could not decrypt keystore: invalid password",
			wantErrs: []string{"validator sidecar container exited with code 1", "could not decrypt keystore: invalid password"},
		},
		{
			name:     "keymanager never answers",
			logs:     "could not connect to beacon node",
			cancel:   errors.New("suite timed out"),
			wantErrs: []string{"wait for validator sidecar", "suite timed out", "last log lines:\ncould not connect to beacon node"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			docker := newDocker()
			if test.state != nil {
				docker.State = test.state
			}
			docker.Logs = test.logs
			ctx := t.Context()
			if test.cancel != nil {
				var cancel context.CancelCauseFunc
				ctx, cancel = context.WithCancelCause(ctx)
				cancel(test.cancel)
			}

			_, err := start(ctx, testConfig(), docker)
			for _, want := range test.wantErrs {
				require.ErrorContains(t, err, want)
			}
			if test.cancel != nil {
				require.ErrorIs(t, err, test.cancel)
			}
			require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
		})
	}
}

func TestVoluntaryExit(t *testing.T) {
	for _, test := range []struct {
		name     string
		output   string
		exitCode int
		wantErrs []string
	}{
		{
			name:   "accepted",
			output: "level=info msg=\"Voluntary exit was successful for the accounts listed:\\n0xabc\\n\"\n",
		},
		{
			name:     "rejected",
			output:   "level=error msg=\"voluntary exit failed for account 0xabc\"\nlevel=info msg=\"No successful voluntary exits\"\n",
			wantErrs: []string{"voluntary exit was not accepted: ", "No successful voluntary exits"},
		},
		{
			name:     "command fails",
			output:   "could not dial beacon node",
			exitCode: 1,
			wantErrs: []string{"exit 1", "could not dial beacon node"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			validator, docker := startWithKeymanager(t)
			t.Cleanup(func() { _ = validator.Close() })
			docker.ExecOutput = test.output
			docker.ExecExitCode = test.exitCode

			err := validator.VoluntaryExit(t.Context(), "0xabc")
			if len(test.wantErrs) == 0 {
				require.NoError(t, err)
			}
			for _, want := range test.wantErrs {
				require.ErrorContains(t, err, want)
			}
			require.Equal(t, [][]string{{
				"/validator", "accounts", "voluntary-exit",
				"--accept-terms-of-use",
				"--wallet-dir=/wallet",
				"--wallet-password-file=/wallet-password.txt",
				"--beacon-rpc-provider=host.docker.internal:4000",
				"--public-keys=0xabc",
				"--force-exit",
			}}, docker.Execs)
		})
	}
}

func TestFixtureFiles(t *testing.T) {
	files, err := fixtureFiles([]byte("PRESET_BASE: minimal\n"), []sidecar.File{{
		Name: "/tmp/keystore-m_12381_238_0_0-1.json",
		Body: []byte(`{"crypto":{}}`),
	}})
	require.NoError(t, err)
	names := make([]string, len(files))
	for index, file := range files {
		names[index] = file.Name
	}
	require.Equal(t, []string{
		"/start-validator.sh",
		"/wallet-password.txt",
		"/network-configs/config.yaml",
		"/keys/keystore-m_12381_238_0_0-1.json",
	}, names)

	_, err = fixtureFiles(nil, nil)
	require.EqualError(t, err, "chain config is empty")
}

func TestParseAuthToken(t *testing.T) {
	for _, raw := range []string{"0xabc\n", "0xabc\n\n", "  0xabc  ", "old\n0xabc\n"} {
		token, err := parseAuthToken([]byte(raw))
		require.NoError(t, err, "token file %q", raw)
		require.Equal(t, "0xabc", token, "token file %q", raw)
	}
	for _, raw := range []string{"", "\n\n"} {
		_, err := parseAuthToken([]byte(raw))
		require.EqualError(t, err, "validator auth token is empty", "token file %q", raw)
	}
}

func TestReadChainConfigNeedsServiceID(t *testing.T) {
	_, err := readChainConfig(t.Context(), newDocker(), " ")
	require.EqualError(t, err, "consensus service has no ID")
}

// startWithKeymanager starts the sidecar against a fake whose keymanager API
// answers with the auth token.
func startWithKeymanager(t *testing.T) (*Sidecar, *sidecartest.Docker) {
	const token = "0xabc"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "Bearer "+token, request.Header.Get("Authorization"))
		assert.Equal(t, "/qrl/v1/keystores", request.URL.Path)
		_, _ = writer.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)

	docker := newDocker()
	docker.HostPort = endpoint.Port()
	docker.Files[authTokenPath] = []byte(token + "\n")

	validator, err := start(t.Context(), testConfig(), docker)
	require.NoError(t, err)
	return validator, docker
}

func testConfig() Config {
	return Config{
		Image:              "validator:test",
		BeaconURL:          "http://127.0.0.1:3500",
		BeaconGRPC:         "127.0.0.1:4000",
		ConsensusServiceID: "consensus-service",
	}
}

// newDocker returns a fake that also serves the devnet's consensus container
// with its chain config.
func newDocker() *sidecartest.Docker {
	docker := sidecartest.NewDocker()
	docker.Containers = []containertypes.Summary{{ID: "consensus"}}
	docker.Files[chainConfigPath] = []byte("PRESET_BASE: minimal\n")
	return docker
}
