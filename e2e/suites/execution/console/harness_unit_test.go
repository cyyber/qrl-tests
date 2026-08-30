package console

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"sync"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestParseSuiteResult(t *testing.T) {
	require.NoError(t, parseSuiteResult("api", []byte("CONSOLE_E2E_PASS api")))
	for name, output := range map[string]string{
		"failure":              "CONSOLE_E2E_FAIL api",
		"success then failure": "CONSOLE_E2E_PASS api\nCONSOLE_E2E_FAIL api unexpected callback",
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, parseSuiteResult("api", []byte(output)))
		})
	}
}

const fakeContainerID = "console-container"

type fakeConsoleEngine struct {
	spec             consoleContainerSpec
	calls            []string
	process          fakeConsoleProcessConfig
	onStart          func()
	createErr        error
	copyErr          error
	startErr         error
	removeErr        error
	removeContextErr error
}

func (engine *fakeConsoleEngine) copyFixtures(_ context.Context, containerID string) error {
	engine.calls = append(engine.calls, "copy:"+containerID)
	return engine.copyErr
}

func (engine *fakeConsoleEngine) create(_ context.Context, spec consoleContainerSpec) (string, error) {
	engine.calls = append(engine.calls, "create")
	engine.spec = spec
	if engine.createErr != nil {
		return "", engine.createErr
	}
	return fakeContainerID, nil
}

func (engine *fakeConsoleEngine) start(
	ctx context.Context,
	containerID string,
) (consoleContainerProcess, error) {
	engine.calls = append(engine.calls, "start:"+containerID)
	if engine.startErr != nil {
		return nil, engine.startErr
	}
	process := newFakeConsoleProcess(ctx, engine.process)
	if engine.onStart != nil {
		engine.onStart()
	}
	return process, nil
}

func (engine *fakeConsoleEngine) remove(ctx context.Context, containerID string) error {
	engine.calls = append(engine.calls, "remove:"+containerID)
	engine.removeContextErr = ctx.Err()
	return engine.removeErr
}

type fakeDockerClient struct {
	calls         []string
	createOptions dockerclient.ContainerCreateOptions
	copyOptions   dockerclient.CopyToContainerOptions
	copyContent   []byte
	serverConn    net.Conn
}

func (client *fakeDockerClient) ContainerCreate(
	_ context.Context,
	options dockerclient.ContainerCreateOptions,
) (dockerclient.ContainerCreateResult, error) {
	client.calls = append(client.calls, "create")
	client.createOptions = options
	return dockerclient.ContainerCreateResult{ID: "container-id"}, nil
}

func (client *fakeDockerClient) CopyToContainer(
	_ context.Context,
	_ string,
	options dockerclient.CopyToContainerOptions,
) (dockerclient.CopyToContainerResult, error) {
	client.calls = append(client.calls, "copy")
	client.copyOptions = options
	content, err := io.ReadAll(options.Content)
	client.copyContent = content
	return dockerclient.CopyToContainerResult{}, err
}

func (client *fakeDockerClient) ContainerAttach(
	_ context.Context,
	_ string,
	_ dockerclient.ContainerAttachOptions,
) (dockerclient.ContainerAttachResult, error) {
	client.calls = append(client.calls, "attach")
	clientConn, serverConn := net.Pipe()
	client.serverConn = serverConn
	return dockerclient.ContainerAttachResult{
		HijackedResponse: dockerclient.NewHijackedResponse(clientConn, "application/vnd.docker.multiplexed-stream"),
	}, nil
}

func (client *fakeDockerClient) ContainerStart(
	_ context.Context,
	_ string,
	_ dockerclient.ContainerStartOptions,
) (dockerclient.ContainerStartResult, error) {
	client.calls = append(client.calls, "start")
	return dockerclient.ContainerStartResult{}, nil
}

func (client *fakeDockerClient) ContainerWait(
	_ context.Context,
	_ string,
	options dockerclient.ContainerWaitOptions,
) dockerclient.ContainerWaitResult {
	client.calls = append(client.calls, "wait:"+string(options.Condition))
	result := make(chan containertypes.WaitResponse, 1)
	result <- containertypes.WaitResponse{}
	return dockerclient.ContainerWaitResult{Result: result, Error: make(chan error)}
}

func (client *fakeDockerClient) ContainerRemove(
	_ context.Context,
	_ string,
	options dockerclient.ContainerRemoveOptions,
) (dockerclient.ContainerRemoveResult, error) {
	client.calls = append(client.calls, fmt.Sprintf("remove:%t", options.Force))
	return dockerclient.ContainerRemoveResult{}, nil
}

func TestDockerConsoleEngine(t *testing.T) {
	client := &fakeDockerClient{}
	t.Cleanup(func() {
		if client.serverConn != nil {
			_ = client.serverConn.Close()
		}
	})
	engine := dockerConsoleEngine{client: client}

	containerID, err := engine.create(t.Context(), consoleContainerSpec{
		image:    "registry.example/go-qrl@sha256:digest",
		endpoint: "http://127.0.0.1:8545",
		scenario: "api",
	})
	require.NoError(t, err)
	require.Equal(t, "container-id", containerID)
	require.Equal(t, "registry.example/go-qrl@sha256:digest", client.createOptions.Config.Image)
	require.Equal(t, []string{"gqrl"}, client.createOptions.Config.Entrypoint)
	require.Equal(t, []string{
		"attach",
		"--datadir", "/tmp/qrl-tests-console",
		"--jspath", "/tmp/qrl-tests-js",
		"--exec", "loadScript('harness.js');loadScript('assertions.js');loadScript('api.js')",
		"http://host.docker.internal:8545",
	}, client.createOptions.Config.Cmd)
	require.True(t, client.createOptions.Config.AttachStdout)
	require.True(t, client.createOptions.Config.AttachStderr)
	require.Equal(t, []string{"host.docker.internal:host-gateway"}, client.createOptions.HostConfig.ExtraHosts)

	require.NoError(t, engine.copyFixtures(t.Context(), containerID))
	require.Equal(t, "/tmp", client.copyOptions.DestinationPath)
	archive := tar.NewReader(bytes.NewReader(client.copyContent))
	contents := make(map[string]string)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if header.Typeflag != tar.TypeReg {
			continue
		}
		content, err := io.ReadAll(archive)
		require.NoError(t, err)
		contents[header.Name] = string(content)
	}
	require.Len(t, contents, 3)
	for _, name := range []string{"api.js", "assertions.js", "harness.js"} {
		expected, err := fs.ReadFile(consoleFixtures, "testdata/console/"+name)
		require.NoError(t, err)
		require.Equal(t, string(expected), contents["qrl-tests-js/"+name])
	}

	process, err := engine.start(t.Context(), containerID)
	require.NoError(t, err)
	require.NoError(t, process.wait())
	process.close()
	require.NoError(t, engine.remove(t.Context(), containerID))
	require.Equal(t, []string{"create", "copy", "attach", "wait:next-exit", "start", "remove:true"}, client.calls)
}

func TestConsoleContainerEndpoint(t *testing.T) {
	for name, testCase := range map[string]struct {
		endpoint string
		want     string
		wantErr  bool
	}{
		"IPv4 loopback": {
			endpoint: "http://127.23.45.67:8545",
			want:     "http://host.docker.internal:8545",
		},
		"localhost WebSocket": {
			endpoint: "ws://localhost:8546",
			want:     "ws://host.docker.internal:8546",
		},
		"IPv6 loopback": {
			endpoint: "ws://[::1]:8546",
			want:     "ws://host.docker.internal:8546",
		},
		"non-loopback": {
			endpoint: "https://rpc.example:443",
			want:     "https://rpc.example:443",
		},
		"missing host": {
			endpoint: "http:///rpc",
			wantErr:  true,
		},
		"invalid URL": {
			endpoint: "http://%zz",
			wantErr:  true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := consoleContainerEndpoint(testCase.endpoint)
			if testCase.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.want, got)
		})
	}
}

type fakeConsoleProcess struct {
	ctx       context.Context
	cancel    context.CancelFunc
	config    fakeConsoleProcessConfig
	closed    chan struct{}
	closeOnce sync.Once
}

type fakeConsoleProcessConfig struct {
	output  string
	readErr error
	waitErr error
	pending bool
}

func newFakeConsoleProcess(ctx context.Context, config fakeConsoleProcessConfig) *fakeConsoleProcess {
	processCtx, cancel := context.WithCancel(ctx)
	return &fakeConsoleProcess{
		ctx: processCtx, cancel: cancel, config: config, closed: make(chan struct{}),
	}
}

func (process *fakeConsoleProcess) readOutput(destination io.Writer) error {
	if _, err := io.WriteString(destination, process.config.output); err != nil {
		return err
	}
	if process.config.readErr != nil {
		return process.config.readErr
	}
	<-process.closed
	return nil
}

func (process *fakeConsoleProcess) wait() error {
	if !process.config.pending {
		process.close()
		return process.config.waitErr
	}
	<-process.ctx.Done()
	process.close()
	return process.ctx.Err()
}

func (process *fakeConsoleProcess) close() {
	process.closeOnce.Do(func() {
		process.cancel()
		close(process.closed)
	})
}

func runFakeSuite(ctx context.Context, engine consoleContainerEngine) error {
	return runSuiteWithEngine(ctx, "image", "http://127.0.0.1:8545", "api", engine)
}

var fakeConsoleLifecycle = []string{
	"create",
	"copy:" + fakeContainerID,
	"start:" + fakeContainerID,
	"remove:" + fakeContainerID,
}

func TestRunSuite(t *testing.T) {
	engine := &fakeConsoleEngine{process: fakeConsoleProcessConfig{output: resultPrefix + "api\n"}}
	err := runSuiteWithEngine(
		t.Context(),
		"registry.example/go-qrl@sha256:digest",
		"http://127.0.0.1:8545",
		"api",
		engine,
	)
	require.NoError(t, err)
	require.Equal(t, consoleContainerSpec{
		image:    "registry.example/go-qrl@sha256:digest",
		endpoint: "http://127.0.0.1:8545",
		scenario: "api",
	}, engine.spec)
	require.Equal(t, fakeConsoleLifecycle, engine.calls)
}

func TestRunSuiteFailures(t *testing.T) {
	processErr := errors.New("process failed")
	outputErr := errors.New("output failed")
	cleanupErr := errors.New("cleanup failed")
	for _, testCase := range []struct {
		name       string
		output     string
		readErr    error
		waitErr    error
		pending    bool
		removeErr  error
		wantErr    error
		wantDetail string
	}{
		{name: "script", output: failurePrefix + "api helper failure\n", wantDetail: "emitted a failure marker"},
		{name: "process", waitErr: processErr, wantErr: processErr},
		{name: "output", readErr: outputErr, pending: true, wantErr: outputErr},
		{name: "cleanup", output: resultPrefix + "api\n", removeErr: cleanupErr, wantErr: cleanupErr},
		{
			name:       "script and cleanup",
			output:     failurePrefix + "api helper failure\n",
			removeErr:  cleanupErr,
			wantErr:    cleanupErr,
			wantDetail: "emitted a failure marker",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			engine := &fakeConsoleEngine{
				process: fakeConsoleProcessConfig{
					output:  testCase.output,
					readErr: testCase.readErr,
					waitErr: testCase.waitErr,
					pending: testCase.pending,
				},
				removeErr: testCase.removeErr,
			}
			err := runFakeSuite(t.Context(), engine)
			require.Error(t, err)
			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)
			}
			if testCase.wantDetail != "" {
				require.ErrorContains(t, err, testCase.wantDetail)
			}
			require.Equal(t, fakeConsoleLifecycle, engine.calls)
		})
	}
}

func TestRunSuiteSetupFailures(t *testing.T) {
	setupErr := errors.New("setup failed")
	for _, testCase := range []struct {
		name      string
		engine    fakeConsoleEngine
		wantCalls []string
	}{
		{name: "create", engine: fakeConsoleEngine{createErr: setupErr}, wantCalls: []string{"create"}},
		{name: "copy", engine: fakeConsoleEngine{copyErr: setupErr}, wantCalls: []string{
			"create", "copy:" + fakeContainerID, "remove:" + fakeContainerID,
		}},
		{name: "start", engine: fakeConsoleEngine{startErr: setupErr}, wantCalls: []string{
			"create", "copy:" + fakeContainerID, "start:" + fakeContainerID, "remove:" + fakeContainerID,
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := runFakeSuite(t.Context(), &testCase.engine)
			require.ErrorIs(t, err, setupErr)
			require.Equal(t, testCase.wantCalls, testCase.engine.calls)
		})
	}
}

func TestRunSuiteCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	engine := &fakeConsoleEngine{
		process: fakeConsoleProcessConfig{pending: true},
		onStart: cancel,
	}
	err := runFakeSuite(ctx, engine)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, fakeConsoleLifecycle, engine.calls)
	require.NoError(t, engine.removeContextErr)
}
