package sidecar

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/sidecartest"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	dockerclient "github.com/moby/moby/client"
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
	require.Equal(t, []string{"start.sh", "config/network/config.yaml"}, names)

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

func TestRunRemovesContainerOnFailure(t *testing.T) {
	for _, test := range []struct {
		name        string
		exitCode    int64
		waitMessage string
		neverExits  bool
		logs        string
		cancel      error
		wantExit    bool
		wantErr     string
	}{
		{
			name:     "non-zero exit",
			exitCode: 1,
			logs:     "tool failed",
			wantExit: true,
			wantErr:  "test sidecar container exited with code 1\nlast log lines:\ntool failed",
		},
		{
			name:        "wait error",
			waitMessage: "container removed before it exited",
			wantErr:     "wait for test sidecar: container removed before it exited",
		},
		{
			name:       "cancelled",
			neverExits: true,
			cancel:     errors.New("suite timed out"),
			wantErr:    "register test sidecar exit waiter: suite timed out",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			docker := sidecartest.NewDocker()
			docker.ExitCode = test.exitCode
			docker.WaitMessage = test.waitMessage
			docker.NeverExits = test.neverExits
			docker.Logs = test.logs
			ctx := t.Context()
			if test.cancel != nil {
				var cancel context.CancelCauseFunc
				ctx, cancel = context.WithCancelCause(ctx)
				cancel(test.cancel)
			}

			_, err := Run(ctx, docker, testSpec())
			require.EqualError(t, err, test.wantErr)
			if test.wantExit {
				var exitErr *ExitError
				require.ErrorAs(t, err, &exitErr)
			}
			if test.cancel != nil {
				require.ErrorIs(t, err, test.cancel)
			}
			require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
		})
	}
}

func TestAttachStreamsTheProcess(t *testing.T) {
	for _, test := range []struct {
		name        string
		stdin       bool
		exitCode    int64
		waitMessage string
		wantErr     string
		wantExit    bool
	}{
		{name: "output only"},
		{name: "with stdin", stdin: true},
		{name: "failed exit", exitCode: 2, wantErr: "test sidecar container exited with code 2", wantExit: true},
		{name: "wait error", waitMessage: "container removed", wantErr: "wait for test sidecar: container removed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			docker := sidecartest.NewDocker()
			docker.AttachOutput = "tool output\n"
			docker.ExitCode = test.exitCode
			docker.WaitMessage = test.waitMessage
			spec := testSpec()
			spec.Cmd = []string{"--mode", "test"}
			spec.Stdin = test.stdin

			process, err := Attach(t.Context(), docker, spec)
			require.NoError(t, err)
			require.Equal(t, []string{"--mode", "test"}, docker.Created.Config.Cmd)
			require.True(t, docker.Created.Config.AttachStdout)
			require.True(t, docker.Created.Config.AttachStderr)
			require.Equal(t, test.stdin, docker.Created.Config.AttachStdin)
			require.Equal(t, test.stdin, docker.Created.Config.OpenStdin)
			require.Equal(t, test.stdin, docker.Created.Config.StdinOnce)
			require.Equal(t, dockerclient.ContainerAttachOptions{Stream: true, Stdin: test.stdin, Stdout: true, Stderr: true}, docker.Attached)

			var output bytes.Buffer
			require.NoError(t, process.Output(&output))
			require.Equal(t, "tool output\n", output.String())
			if test.wantErr == "" {
				require.NoError(t, process.Wait())
			} else {
				err := process.Wait()
				require.EqualError(t, err, test.wantErr)
				var exitErr *ExitError
				require.Equal(t, test.wantExit, errors.As(err, &exitErr))
			}

			require.NoError(t, process.Close())
			require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
		})
	}
}

func TestAttachRemovesContainerOnFailure(t *testing.T) {
	errDaemon := errors.New("daemon unavailable")
	for _, test := range []struct {
		method  string
		wantErr string
	}{
		{method: "ContainerAttach", wantErr: "attach to test sidecar container: daemon unavailable"},
		{method: "ContainerWait", wantErr: "register test sidecar exit waiter: daemon unavailable"},
		{method: "ContainerStart", wantErr: "start test sidecar container: daemon unavailable"},
	} {
		t.Run(test.method, func(t *testing.T) {
			docker := sidecartest.NewDocker()
			docker.Fail[test.method] = errDaemon

			process, err := Attach(t.Context(), docker, testSpec())
			require.Nil(t, process)
			require.EqualError(t, err, test.wantErr)
			require.ErrorIs(t, err, errDaemon)
			require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
		})
	}
}

func TestProcessWaitStops(t *testing.T) {
	cause := errors.New("suite timed out")
	for _, test := range []struct {
		name    string
		stop    func(*Process, context.CancelCauseFunc)
		wantErr error
	}{
		{name: "detached", stop: func(process *Process, _ context.CancelCauseFunc) { process.Detach() }, wantErr: context.Canceled},
		{name: "cancelled", stop: func(_ *Process, cancel context.CancelCauseFunc) { cancel(cause) }, wantErr: cause},
	} {
		t.Run(test.name, func(t *testing.T) {
			docker := sidecartest.NewDocker()
			docker.NeverExits = true
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			process, err := Attach(ctx, docker, testSpec())
			require.NoError(t, err)

			test.stop(process, cancel)
			err = process.Wait()
			require.EqualError(t, err, "wait for test sidecar: "+test.wantErr.Error())
			require.ErrorIs(t, err, test.wantErr)
			require.NoError(t, process.Close())
		})
	}
}

func TestProcessCloseClosesOwnedClient(t *testing.T) {
	docker := sidecartest.NewDocker()
	process, err := Attach(t.Context(), docker, testSpec())
	require.NoError(t, err)
	errClient := errors.New("client close failed")
	closed := false
	process.closeClient = func() error {
		closed = true
		return errClient
	}

	require.ErrorIs(t, process.Close(), errClient)
	require.True(t, closed)
	require.Equal(t, []string{sidecartest.ContainerID}, docker.Removed)
}

func TestPublishedPortErrors(t *testing.T) {
	exited := &containertypes.State{Status: containertypes.StateExited, ExitCode: 1}
	for _, test := range []struct {
		name     string
		noPort   bool
		state    *containertypes.State
		logs     string
		fail     map[string]error
		wantExit bool
		wantErr  string
	}{
		{
			name:     "exited with logs",
			state:    exited,
			logs:     "could not read config\n",
			wantExit: true,
			wantErr:  "test sidecar container exited with code 1\nlast log lines:\ncould not read config",
		},
		{
			name:     "dead without logs",
			state:    &containertypes.State{Status: containertypes.StateDead, Error: "OCI runtime error"},
			wantExit: true,
			wantErr:  "test sidecar container is dead: OCI runtime error",
		},
		{
			name:     "logs unavailable",
			state:    exited,
			fail:     map[string]error{"ContainerLogs": errors.New("daemon unavailable")},
			wantExit: true,
			wantErr:  "test sidecar container exited with code 1\n(logs unavailable: daemon unavailable)",
		},
		{
			name:    "no port in the spec",
			noPort:  true,
			wantErr: "test sidecar publishes no port",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			docker := sidecartest.NewDocker()
			spec := testSpec()
			if test.noPort {
				spec.Port = 0
			}
			container, err := Start(t.Context(), docker, spec)
			require.NoError(t, err)
			if test.state != nil {
				docker.State = test.state
			}
			docker.Logs = test.logs
			maps.Copy(docker.Fail, test.fail)

			_, err = container.PublishedPort(t.Context())
			require.EqualError(t, err, test.wantErr)
			if test.wantExit {
				var exitErr *ExitError
				require.ErrorAs(t, err, &exitErr)
			}
		})
	}
}

func TestPublishedHostPortRequiresNetworkSettings(t *testing.T) {
	port, ok := network.PortFrom(7500, network.TCP)
	require.True(t, ok)
	_, err := publishedHostPort(containertypes.InspectResponse{}, port)
	require.EqualError(t, err, "container has no network settings")

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

func TestFilesIn(t *testing.T) {
	files, err := FilesIn("/keys", []File{
		{Name: "a.json", Body: []byte("a")},
		{Name: " dir/b.json ", Body: []byte("b"), Mode: 0o444},
	})
	require.NoError(t, err)
	require.Equal(t, []File{
		{Name: "/keys/a.json", Body: []byte("a")},
		{Name: "/keys/b.json", Body: []byte("b"), Mode: 0o444},
	}, files)

	for _, name := range []string{"", " ", "/"} {
		_, err := FilesIn("/keys", []File{{Name: name}})
		require.EqualError(t, err, "file name is empty", "name %q", name)
	}
}

func TestExec(t *testing.T) {
	errDaemon := errors.New("daemon unavailable")
	for _, test := range []struct {
		name       string
		exitCode   int
		fail       map[string]error
		wantOutput string
		wantErr    string
	}{
		{name: "success", wantOutput: "tool output"},
		{name: "non-zero exit", exitCode: 1, wantOutput: "tool output", wantErr: "/bin/tool run: exit 1: tool output"},
		{
			name:    "create fails",
			fail:    map[string]error{"ExecCreate": errDaemon},
			wantErr: "create exec /bin/tool run: daemon unavailable",
		},
		{
			name:    "attach fails",
			fail:    map[string]error{"ExecAttach": errDaemon},
			wantErr: "attach exec /bin/tool run: daemon unavailable",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			docker := sidecartest.NewDocker()
			docker.ExecOutput = "tool output"
			docker.ExecExitCode = test.exitCode
			container, err := Start(t.Context(), docker, testSpec())
			require.NoError(t, err)
			maps.Copy(docker.Fail, test.fail)

			output, err := container.Exec(t.Context(), "/bin/tool", "run")
			require.Equal(t, test.wantOutput, output)
			require.Equal(t, [][]string{{"/bin/tool", "run"}}, docker.Execs)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, test.wantErr)
			if len(test.fail) > 0 {
				require.ErrorIs(t, err, errDaemon)
			}
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

func TestProcessCloseInput(t *testing.T) {
	newProcess := func(t *testing.T) (*Process, *writeSignalingConn, net.Conn) {
		t.Helper()
		clientConn, serverConn := net.Pipe()
		connection := &writeSignalingConn{
			Conn:        clientConn,
			started:     make(chan struct{}),
			writeDone:   make(chan struct{}),
			inputClosed: make(chan struct{}),
		}
		processCtx, cancel := context.WithCancel(t.Context())
		process := &Process{
			Container: &Container{name: "test sidecar"},
			ctx:       processCtx,
			cancel:    cancel,
			attach: dockerclient.ContainerAttachResult{
				HijackedResponse: dockerclient.NewHijackedResponse(connection, ""),
			},
		}
		t.Cleanup(process.Detach)
		t.Cleanup(func() { _ = serverConn.Close() })
		return process, connection, serverConn
	}

	t.Run("success", func(t *testing.T) {
		process, connection, serverConn := newProcess(t)
		done := make(chan error, 1)
		go func() { done <- process.CloseInput(t.Context(), "exit\n") }()

		input := make([]byte, len("exit\n"))
		_, err := io.ReadFull(serverConn, input)
		require.NoError(t, err)
		require.Equal(t, "exit\n", string(input))
		require.NoError(t, <-done)
		<-connection.inputClosed
	})

	t.Run("cancellation", func(t *testing.T) {
		process, connection, _ := newProcess(t)
		closeCtx, cancel := context.WithCancelCause(t.Context())
		cancelErr := errors.New("input cancelled")
		done := make(chan error, 1)
		go func() { done <- process.CloseInput(closeCtx, "exit\n") }()
		<-connection.started
		cancel(cancelErr)

		select {
		case err := <-done:
			require.ErrorIs(t, err, cancelErr)
		case <-time.After(time.Second):
			t.Fatal("closing input remained blocked after cancellation")
		}
		select {
		case <-connection.writeDone:
		case <-time.After(time.Second):
			t.Fatal("blocked input writer was not released")
		}
		select {
		case <-connection.inputClosed:
		case <-time.After(time.Second):
			t.Fatal("input was not closed after cancellation")
		}
	})
}

// writeSignalingConn reports when a write starts and ends, and records
// CloseWrite, so tests can hold a write open.
type writeSignalingConn struct {
	net.Conn
	started     chan struct{}
	writeDone   chan struct{}
	inputClosed chan struct{}
}

func (connection *writeSignalingConn) Write(data []byte) (int, error) {
	close(connection.started)
	written, err := connection.Conn.Write(data)
	close(connection.writeDone)
	return written, err
}

func (connection *writeSignalingConn) CloseWrite() error {
	close(connection.inputClosed)
	return nil
}
