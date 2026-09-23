package sidecar

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/sidecartest"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
)

func testSpec() Spec {
	return Spec{
		Name:       "test sidecar",
		Image:      "sidecar:test",
		Entrypoint: []string{"/bin/sh", "/start.sh"},
		Env:        []string{"MODE=test"},
		Files: []File{
			{Name: "/start.sh", Body: []byte("#!/bin/sh\n"), Mode: 0o755},
			{Name: "/config/network/config.yaml", Body: []byte("PRESET_BASE: minimal\n")},
		},
		Port: 7500,
	}
}

func TestStartCreatesPublishedContainer(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.HostPort = "32765"

	container, err := Start(t.Context(), docker, testSpec())
	require.NoError(t, err)

	port, ok := network.PortFrom(7500, network.TCP)
	require.True(t, ok)
	require.Equal(t, "sidecar:test", docker.Created.Config.Image)
	require.Equal(t, []string{"/bin/sh", "/start.sh"}, docker.Created.Config.Entrypoint)
	require.Equal(t, []string{"MODE=test"}, docker.Created.Config.Env)
	require.Equal(t, []string{"host.docker.internal:host-gateway"}, docker.Created.HostConfig.ExtraHosts)
	require.Equal(t, map[string]string{"qrl-tests.sidecar": "test sidecar"}, docker.Created.Config.Labels)
	require.Equal(t, network.PortMap{port: {{HostIP: netip.MustParseAddr("127.0.0.1")}}}, docker.Created.HostConfig.PortBindings)

	names, err := docker.ArchiveNames()
	require.NoError(t, err)
	require.Equal(t, []string{"start.sh", "config/network/config.yaml"}, names,
		"directory entries would reset existing directories; Docker creates missing parents itself")

	hostPort, err := container.PublishedPort(t.Context())
	require.NoError(t, err)
	require.Equal(t, "32765", hostPort)

	require.NoError(t, container.Close())
	require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
}

func TestStartRemovesContainerOnFailure(t *testing.T) {
	for _, test := range []struct {
		name     string
		setup    func(*sidecartest.Docker)
		wantErrs []string
	}{
		{
			name:     "start fails",
			setup:    func(docker *sidecartest.Docker) { docker.StartErr = errors.New("no space left") },
			wantErrs: []string{"start test sidecar container: no space left"},
		},
		{
			name: "start and removal fail",
			setup: func(docker *sidecartest.Docker) {
				docker.StartErr = errors.New("no space left")
				docker.RemoveErr = errors.New("daemon unavailable")
			},
			wantErrs: []string{"no space left", "remove test sidecar container: daemon unavailable"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			docker := sidecartest.NewDocker()
			test.setup(docker)

			_, err := Start(t.Context(), docker, testSpec())
			for _, want := range test.wantErrs {
				require.ErrorContains(t, err, want)
			}
			require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
		})
	}
}

func TestPublishedPortReportsExit(t *testing.T) {
	docker := sidecartest.NewDocker()
	container, err := Start(t.Context(), docker, testSpec())
	require.NoError(t, err)

	docker.State = &containertypes.State{Status: containertypes.StateExited, ExitCode: 1}
	docker.Logs = "could not decrypt keystore: invalid password\n"

	_, err = container.PublishedPort(t.Context())
	var exitErr *ExitError
	require.ErrorAs(t, err, &exitErr)
	require.EqualError(t, err, "test sidecar container exited with code 1\nlast log lines:\ncould not decrypt keystore: invalid password")

	docker.State = &containertypes.State{Status: containertypes.StateDead, Error: "OCI runtime error"}
	docker.Logs = ""
	_, err = container.PublishedPort(t.Context())
	require.EqualError(t, err, "test sidecar container is dead: OCI runtime error")
}

func TestExecReportsExitCode(t *testing.T) {
	for _, test := range []struct {
		name     string
		exitCode int
		wantErr  string
	}{
		{name: "success", exitCode: 0},
		{name: "failure", exitCode: 1, wantErr: "/validator accounts list: exit 1: no wallet found"},
	} {
		t.Run(test.name, func(t *testing.T) {
			docker := sidecartest.NewDocker()
			docker.ExecOutput = "no wallet found"
			docker.ExecExitCode = test.exitCode
			container, err := Start(t.Context(), docker, testSpec())
			require.NoError(t, err)

			output, err := container.Exec(t.Context(), "/validator", "accounts", "list")
			require.Equal(t, "no wallet found", output)
			require.Equal(t, [][]string{{"/validator", "accounts", "list"}}, docker.Execs)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, test.wantErr)
		})
	}
}

func TestReadFile(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.Files["/wallet/auth-token"] = []byte("0xabc\n")
	container, err := Start(t.Context(), docker, testSpec())
	require.NoError(t, err)

	body, err := container.ReadFile(t.Context(), "/wallet/auth-token")
	require.NoError(t, err)
	require.Equal(t, []byte("0xabc\n"), body)

	_, err = container.ReadFile(t.Context(), "/wallet/missing")
	require.ErrorContains(t, err, "no such file")

	_, err = container.ReadFile(t.Context(), "/wallet")
	require.EqualError(t, err, "archive does not contain /wallet", "a directory is not a file")
}

func TestPublishedHostPortRequiresNetworkSettings(t *testing.T) {
	port, ok := network.PortFrom(7500, network.TCP)
	require.True(t, ok)
	_, err := publishedHostPort(containertypes.InspectResponse{}, port)
	require.ErrorContains(t, err, "network settings")

	_, err = publishedHostPort(containertypes.InspectResponse{NetworkSettings: &containertypes.NetworkSettings{}}, port)
	require.EqualError(t, err, "container port 7500/tcp is not published")
}

func TestPublishedPortRequiresASpecPort(t *testing.T) {
	spec := testSpec()
	spec.Port = 0
	container, err := Start(t.Context(), sidecartest.NewDocker(), spec)
	require.NoError(t, err)

	_, err = container.PublishedPort(t.Context())
	require.EqualError(t, err, "test sidecar publishes no port")
}

func TestRunWaitsForSuccessfulExit(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.Files["/keys/deposit_data-1.json"] = []byte("[]")
	docker.Files["/keys/keystore-1.json"] = []byte("{}")

	container, err := Run(t.Context(), docker, testSpec())
	require.NoError(t, err)
	require.Empty(t, docker.Removed, "a successful run keeps the container for its output")

	files, err := container.ReadDir(t.Context(), "/keys")
	require.NoError(t, err)
	require.Equal(t, []File{
		{Name: "/keys/deposit_data-1.json", Body: []byte("[]"), Mode: 0o600},
		{Name: "/keys/keystore-1.json", Body: []byte("{}"), Mode: 0o600},
	}, files)

	require.NoError(t, container.Close())
	require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
}

func TestRunReportsFailedExit(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.ExitCode = 1
	docker.Logs = "insufficient funds for deposit"

	_, err := Run(t.Context(), docker, testSpec())
	var exitErr *ExitError
	require.ErrorAs(t, err, &exitErr)
	require.EqualError(t, err, "test sidecar container exited with code 1\nlast log lines:\ninsufficient funds for deposit")
	require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
}

func TestRunReportsCancellationWithLogs(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.NeverExits = true
	docker.Logs = "waiting for execution client"
	ctx, cancel := context.WithCancelCause(t.Context())
	cancelErr := errors.New("suite timed out")
	cancel(cancelErr)

	_, err := Run(ctx, docker, testSpec())
	require.ErrorIs(t, err, cancelErr)
	require.EqualError(t, err, "wait for test sidecar: suite timed out\nlast log lines:\nwaiting for execution client")
	require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
}

func TestWithLogsReportsUnavailableLogs(t *testing.T) {
	docker := sidecartest.NewDocker()
	container, err := Start(t.Context(), docker, testSpec())
	require.NoError(t, err)
	docker.State = &containertypes.State{Status: containertypes.StateExited, ExitCode: 1}
	docker.LogsErr = errors.New("daemon unavailable")

	_, err = container.PublishedPort(t.Context())
	require.EqualError(t, err, "test sidecar container exited with code 1\n(logs unavailable: daemon unavailable)")
}

func TestRunReportsWaitError(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.WaitMessage = "container removed before it exited"

	_, err := Run(t.Context(), docker, testSpec())
	require.EqualError(t, err, "wait for test sidecar: container removed before it exited")
	require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
}

func TestExecStopsWithContext(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.ExecNeverExits = true
	container, err := Start(t.Context(), docker, testSpec())
	require.NoError(t, err)
	ctx, cancel := context.WithCancelCause(t.Context())
	cancelErr := errors.New("spec timed out")

	done := make(chan error, 1)
	go func() {
		_, err := container.Exec(ctx, "/validator", "accounts", "voluntary-exit")
		done <- err
	}()
	cancel(cancelErr)

	select {
	case err := <-done:
		require.ErrorIs(t, err, cancelErr)
		require.EqualError(t, err, "exec /validator accounts voluntary-exit: spec timed out")
	case <-time.After(5 * time.Second):
		t.Fatal("Exec kept running after its context ended")
	}
}

func TestExecReportsDockerFailures(t *testing.T) {
	execErr := errors.New("daemon unavailable")
	for _, test := range []struct {
		name    string
		setup   func(*sidecartest.Docker)
		wantErr string
	}{
		{
			name:    "create",
			setup:   func(docker *sidecartest.Docker) { docker.ExecCreateErr = execErr },
			wantErr: "create exec /validator accounts list: daemon unavailable",
		},
		{
			name:    "attach",
			setup:   func(docker *sidecartest.Docker) { docker.ExecAttachErr = execErr },
			wantErr: "attach exec /validator accounts list: daemon unavailable",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			docker := sidecartest.NewDocker()
			container, err := Start(t.Context(), docker, testSpec())
			require.NoError(t, err)
			test.setup(docker)

			_, err = container.Exec(t.Context(), "/validator", "accounts", "list")
			require.ErrorIs(t, err, execErr)
			require.EqualError(t, err, test.wantErr)
		})
	}
}
