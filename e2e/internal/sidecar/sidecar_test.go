package sidecar

import (
	"context"
	"errors"
	"maps"
	"net/netip"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/sidecartest"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
)

var _ Client = (*sidecartest.Docker)(nil)

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
	errNoSpace := errors.New("no space left")
	errDaemon := errors.New("daemon unavailable")
	for _, test := range []struct {
		name     string
		fail     map[string]error
		wantErrs []string
	}{
		{
			name:     "start fails",
			fail:     map[string]error{"ContainerStart": errNoSpace},
			wantErrs: []string{"start test sidecar container: no space left"},
		},
		{
			name:     "start and removal fail",
			fail:     map[string]error{"ContainerStart": errNoSpace, "ContainerRemove": errDaemon},
			wantErrs: []string{"no space left", "remove test sidecar container: daemon unavailable"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			docker := sidecartest.NewDocker()
			maps.Copy(docker.Fail, test.fail)

			_, err := Start(t.Context(), docker, testSpec())
			for _, want := range test.wantErrs {
				require.ErrorContains(t, err, want)
			}
			require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
		})
	}
}

func TestRunWaitsForSuccessfulExit(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.Files["/out/a.json"] = []byte("[]")
	docker.Files["/out/b.json"] = []byte("{}")

	container, err := Run(t.Context(), docker, testSpec())
	require.NoError(t, err)
	require.Empty(t, docker.Removed, "a successful run keeps the container for its output")

	files, err := container.ReadDir(t.Context(), "/out")
	require.NoError(t, err)
	require.Equal(t, []File{
		{Name: "/out/a.json", Body: []byte("[]"), Mode: 0o600},
		{Name: "/out/b.json", Body: []byte("{}"), Mode: 0o600},
	}, files)

	require.NoError(t, container.Close())
	require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
}

func TestRunReportsFailedExit(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.ExitCode = 1
	docker.Logs = "tool failed"

	_, err := Run(t.Context(), docker, testSpec())
	var exitErr *ExitError
	require.ErrorAs(t, err, &exitErr)
	require.EqualError(t, err, "test sidecar container exited with code 1\nlast log lines:\ntool failed")
	require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
}

func TestRunReportsWaitError(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.WaitMessage = "container removed before it exited"

	_, err := Run(t.Context(), docker, testSpec())
	require.EqualError(t, err, "wait for test sidecar: container removed before it exited")
	require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
}

func TestRunReportsCancellationWithLogs(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.NeverExits = true
	docker.Logs = "waiting for peer"
	ctx, cancel := context.WithCancelCause(t.Context())
	cancelErr := errors.New("suite timed out")
	cancel(cancelErr)

	_, err := Run(ctx, docker, testSpec())
	require.ErrorIs(t, err, cancelErr)
	require.EqualError(t, err, "wait for test sidecar: suite timed out\nlast log lines:\nwaiting for peer")
	require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
}

func TestPublishedPortReportsExit(t *testing.T) {
	docker := sidecartest.NewDocker()
	container, err := Start(t.Context(), docker, testSpec())
	require.NoError(t, err)

	docker.State = &containertypes.State{Status: containertypes.StateExited, ExitCode: 1}
	docker.Logs = "could not read config\n"

	_, err = container.PublishedPort(t.Context())
	var exitErr *ExitError
	require.ErrorAs(t, err, &exitErr)
	require.EqualError(t, err, "test sidecar container exited with code 1\nlast log lines:\ncould not read config")

	docker.State = &containertypes.State{Status: containertypes.StateDead, Error: "OCI runtime error"}
	docker.Logs = ""
	_, err = container.PublishedPort(t.Context())
	require.EqualError(t, err, "test sidecar container is dead: OCI runtime error")
}

func TestPublishedPortRequiresASpecPort(t *testing.T) {
	spec := testSpec()
	spec.Port = 0
	container, err := Start(t.Context(), sidecartest.NewDocker(), spec)
	require.NoError(t, err)

	_, err = container.PublishedPort(t.Context())
	require.EqualError(t, err, "test sidecar publishes no port")
}

func TestPublishedHostPortRequiresNetworkSettings(t *testing.T) {
	port, ok := network.PortFrom(7500, network.TCP)
	require.True(t, ok)
	_, err := publishedHostPort(containertypes.InspectResponse{}, port)
	require.ErrorContains(t, err, "network settings")

	_, err = publishedHostPort(containertypes.InspectResponse{NetworkSettings: &containertypes.NetworkSettings{}}, port)
	require.EqualError(t, err, "container port 7500/tcp is not published")
}

func TestReadFile(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.Files["/data/token"] = []byte("abc\n")
	container, err := Start(t.Context(), docker, testSpec())
	require.NoError(t, err)

	body, err := container.ReadFile(t.Context(), "/data/token")
	require.NoError(t, err)
	require.Equal(t, []byte("abc\n"), body)

	_, err = container.ReadFile(t.Context(), "/data/missing")
	require.ErrorContains(t, err, "no such file")

	_, err = container.ReadFile(t.Context(), "/data")
	require.EqualError(t, err, "archive does not contain /data", "a directory is not a file")
}

func TestWithLogsReportsUnavailableLogs(t *testing.T) {
	docker := sidecartest.NewDocker()
	container, err := Start(t.Context(), docker, testSpec())
	require.NoError(t, err)
	docker.State = &containertypes.State{Status: containertypes.StateExited, ExitCode: 1}
	docker.Fail["ContainerLogs"] = errors.New("daemon unavailable")

	_, err = container.PublishedPort(t.Context())
	require.EqualError(t, err, "test sidecar container exited with code 1\n(logs unavailable: daemon unavailable)")
}

func TestExecReportsExitCode(t *testing.T) {
	for _, test := range []struct {
		name     string
		exitCode int
		wantErr  string
	}{
		{name: "success", exitCode: 0},
		{name: "failure", exitCode: 1, wantErr: "/bin/tool run: exit 1: tool output"},
	} {
		t.Run(test.name, func(t *testing.T) {
			docker := sidecartest.NewDocker()
			docker.ExecOutput = "tool output"
			docker.ExecExitCode = test.exitCode
			container, err := Start(t.Context(), docker, testSpec())
			require.NoError(t, err)

			output, err := container.Exec(t.Context(), "/bin/tool", "run")
			require.Equal(t, "tool output", output)
			require.Equal(t, [][]string{{"/bin/tool", "run"}}, docker.Execs)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, test.wantErr)
		})
	}
}

func TestExecReportsDockerFailures(t *testing.T) {
	errDaemon := errors.New("daemon unavailable")
	for _, test := range []struct {
		name    string
		fail    map[string]error
		wantErr string
	}{
		{
			name:    "create",
			fail:    map[string]error{"ExecCreate": errDaemon},
			wantErr: "create exec /bin/tool run: daemon unavailable",
		},
		{
			name:    "attach",
			fail:    map[string]error{"ExecAttach": errDaemon},
			wantErr: "attach exec /bin/tool run: daemon unavailable",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			docker := sidecartest.NewDocker()
			container, err := Start(t.Context(), docker, testSpec())
			require.NoError(t, err)
			maps.Copy(docker.Fail, test.fail)

			_, err = container.Exec(t.Context(), "/bin/tool", "run")
			require.ErrorIs(t, err, errDaemon)
			require.EqualError(t, err, test.wantErr)
		})
	}
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
		_, err := container.Exec(ctx, "/bin/tool", "wait")
		done <- err
	}()
	cancel(cancelErr)

	select {
	case err := <-done:
		require.ErrorIs(t, err, cancelErr)
		require.EqualError(t, err, "exec /bin/tool wait: spec timed out")
	case <-time.After(5 * time.Second):
		t.Fatal("Exec kept running after its context ended")
	}
}
