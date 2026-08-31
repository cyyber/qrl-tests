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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestParseSuiteResult(t *testing.T) {
	require.NoError(t, parseSuiteResult("api", []byte("CONSOLE_E2E_PASS api")))
	for name, output := range map[string]string{
		"failure":              "CONSOLE_E2E_FAIL api",
		"GoError":              "GoError: helper failure",
		"success then failure": "CONSOLE_E2E_PASS api\nCONSOLE_E2E_FAIL api unexpected callback",
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, parseSuiteResult("api", []byte(output)))
		})
	}
}

func TestTerminalMarkers(t *testing.T) {
	markers := newTerminalMarkers("events")
	for _, testCase := range []struct {
		name string
		line string
		want terminalSignal
	}{
		{name: "pass", line: "CONSOLE_E2E_PASS events", want: terminalSignalPass},
		{name: "fail", line: "CONSOLE_E2E_FAIL events", want: terminalSignalFail},
		{name: "fail detail", line: "CONSOLE_E2E_FAIL events callback failed", want: terminalSignalFail},
		{name: "GoError", line: "GoError: callback failed", want: terminalSignalGoError},
		{name: "wrong scenario", line: "CONSOLE_E2E_PASS api", want: terminalSignalNone},
		{name: "invalid suffix", line: "CONSOLE_E2E_PASS events extra", want: terminalSignalNone},
		{name: "noise", line: "console ready", want: terminalSignalNone},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, markers.detect([]byte(testCase.line)))
		})
	}
}

func TestWatchedOutput(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		chunks   []string
		complete bool
	}{
		{name: "split marker", chunks: []string{"noise\nCONSOLE_E2E_PA", "SS events\n"}},
		{name: "unterminated marker", chunks: []string{"CONSOLE_E2E_PASS events"}, complete: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output := newConsoleOutput("events", true)
			for _, chunk := range testCase.chunks {
				_, err := io.WriteString(output, chunk)
				require.NoError(t, err)
			}
			if testCase.complete {
				select {
				case <-output.terminal:
					t.Fatal("unterminated marker became visible before output completed")
				default:
				}
			}
			result := consoleOutputResult{output: bytes.Clone(output.data.Bytes())}
			if testCase.complete {
				result = output.complete(nil)
			}

			select {
			case <-output.terminal:
				// Expected.
			default:
				t.Fatal("terminal marker was not detected")
			}
			require.Equal(t, strings.Join(testCase.chunks, ""), string(result.output))
		})
	}
}

const fakeContainerID = "console-container"

var fakeFixtureArchive = []byte("fixture archive")

type fakeConsoleEngine struct {
	config           consoleContainerConfig
	calls            []string
	fixtureArchive   []byte
	process          fakeConsoleProcessConfig
	onStart          func()
	createErr        error
	copyErr          error
	startErr         error
	removeErr        error
	removeContextErr error
	startInteractive bool
	startedProcess   *fakeConsoleProcess
}

func (engine *fakeConsoleEngine) copyFixtures(
	_ context.Context,
	containerID string,
	fixtureArchive []byte,
) error {
	engine.calls = append(engine.calls, "copy:"+containerID)
	engine.fixtureArchive = bytes.Clone(fixtureArchive)
	return engine.copyErr
}

func (engine *fakeConsoleEngine) createContainer(_ context.Context, config consoleContainerConfig) (string, error) {
	engine.calls = append(engine.calls, "create")
	engine.config = config
	if engine.createErr != nil {
		return "", engine.createErr
	}
	return fakeContainerID, nil
}

func (engine *fakeConsoleEngine) startContainer(
	ctx context.Context,
	containerID string,
	interactive bool,
) (consoleContainerProcess, error) {
	engine.calls = append(engine.calls, "start:"+containerID)
	engine.startInteractive = interactive
	if engine.startErr != nil {
		return nil, engine.startErr
	}
	process := newFakeConsoleProcess(ctx, engine.process)
	engine.startedProcess = process
	if engine.onStart != nil {
		engine.onStart()
	}
	return process, nil
}

func (engine *fakeConsoleEngine) removeContainer(ctx context.Context, containerID string) error {
	engine.calls = append(engine.calls, "remove:"+containerID)
	engine.removeContextErr = ctx.Err()
	return engine.removeErr
}

type fakeDockerClient struct {
	attachOptions dockerclient.ContainerAttachOptions
	calls         []string
	createOptions dockerclient.ContainerCreateOptions
	copyOptions   dockerclient.CopyToContainerOptions
	copyContent   []byte
	serverConn    net.Conn
	waitSetupErr  error
}

type writeSignalingConn struct {
	net.Conn
	started     chan struct{}
	writeDone   chan struct{}
	inputClosed chan struct{}
	startOnce   sync.Once
	doneOnce    sync.Once
	closeOnce   sync.Once
}

func (connection *writeSignalingConn) Write(data []byte) (int, error) {
	connection.startOnce.Do(func() { close(connection.started) })
	written, err := connection.Conn.Write(data)
	connection.doneOnce.Do(func() { close(connection.writeDone) })
	return written, err
}

func (connection *writeSignalingConn) CloseWrite() error {
	connection.closeOnce.Do(func() { close(connection.inputClosed) })
	return nil
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
	options dockerclient.ContainerAttachOptions,
) (dockerclient.ContainerAttachResult, error) {
	client.calls = append(client.calls, "attach")
	client.attachOptions = options
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
	errs := make(chan error, 1)
	if client.waitSetupErr != nil {
		errs <- client.waitSetupErr
	} else {
		result <- containertypes.WaitResponse{}
	}
	return dockerclient.ContainerWaitResult{Result: result, Error: errs}
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

	containerID, err := engine.createContainer(t.Context(), consoleContainerConfig{
		image:       "registry.example/go-qrl@sha256:digest",
		endpointURL: "http://127.0.0.1:8545",
		scenario:    "api",
	})
	require.NoError(t, err)
	require.Equal(t, "container-id", containerID)
	require.Equal(t, "registry.example/go-qrl@sha256:digest", client.createOptions.Config.Image)
	require.Equal(t, []string{"gqrl"}, client.createOptions.Config.Entrypoint)
	require.Equal(t, []string{
		"attach",
		"--datadir", "/tmp/qrl-tests-console",
		"--jspath", "/tmp/testdata/console",
		"--exec", "loadScript('harness.js');loadScript('assertions.js');loadScript('api.js')",
		"http://host.docker.internal:8545",
	}, client.createOptions.Config.Cmd)
	require.True(t, client.createOptions.Config.AttachStdout)
	require.True(t, client.createOptions.Config.AttachStderr)
	require.Equal(t, []string{"host.docker.internal:host-gateway"}, client.createOptions.HostConfig.ExtraHosts)

	require.NoError(t, engine.copyFixtures(t.Context(), containerID, fakeFixtureArchive))
	require.Equal(t, "/tmp", client.copyOptions.DestinationPath)
	require.Equal(t, fakeFixtureArchive, client.copyContent)

	process, err := engine.startContainer(t.Context(), containerID, false)
	require.NoError(t, err)
	require.False(t, client.attachOptions.Stdin)
	require.NoError(t, process.wait())
	process.close()
	require.NoError(t, engine.removeContainer(t.Context(), containerID))
	require.Equal(t, []string{"create", "copy", "attach", "wait:next-exit", "start", "remove:true"}, client.calls)
}

func TestDockerConsoleInteractiveConfig(t *testing.T) {
	client := &fakeDockerClient{}
	t.Cleanup(func() {
		if client.serverConn != nil {
			_ = client.serverConn.Close()
		}
	})
	engine := dockerConsoleEngine{client: client}

	containerID, err := engine.createContainer(t.Context(), consoleContainerConfig{
		image:       "registry.example/go-qrl@sha256:digest",
		endpointURL: "ws://127.0.0.1:8546",
		scenario:    "events",
		interactive: true,
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		"attach",
		"--datadir", "/tmp/qrl-tests-console",
		"--jspath", "/tmp/testdata/console",
		"--preload", "harness.js,assertions.js,events.js",
		"ws://host.docker.internal:8546",
	}, client.createOptions.Config.Cmd)
	require.True(t, client.createOptions.Config.AttachStdin)
	require.True(t, client.createOptions.Config.OpenStdin)
	require.True(t, client.createOptions.Config.StdinOnce)

	process, err := engine.startContainer(t.Context(), containerID, true)
	require.NoError(t, err)
	require.True(t, client.attachOptions.Stdin)
	require.NoError(t, process.wait())
	process.close()
}

func TestDockerConsoleWaitFailure(t *testing.T) {
	waitErr := errors.New("wait registration failed")
	client := &fakeDockerClient{waitSetupErr: waitErr}
	t.Cleanup(func() {
		if client.serverConn != nil {
			_ = client.serverConn.Close()
		}
	})

	process, err := (dockerConsoleEngine{client: client}).startContainer(
		t.Context(),
		"container-id",
		false,
	)

	require.Nil(t, process)
	require.ErrorIs(t, err, waitErr)
	require.Equal(t, []string{"attach", "wait:next-exit"}, client.calls)
}

func TestDockerConsoleRequestExit(t *testing.T) {
	newProcess := func(t *testing.T) (*dockerConsoleProcess, *writeSignalingConn, net.Conn) {
		t.Helper()
		clientConn, serverConn := net.Pipe()
		connection := &writeSignalingConn{
			Conn:        clientConn,
			started:     make(chan struct{}),
			writeDone:   make(chan struct{}),
			inputClosed: make(chan struct{}),
		}
		processCtx, cancelProcess := context.WithCancel(t.Context())
		process := &dockerConsoleProcess{
			ctx:    processCtx,
			cancel: cancelProcess,
			attach: dockerclient.ContainerAttachResult{
				HijackedResponse: dockerclient.NewHijackedResponse(connection, ""),
			},
		}
		t.Cleanup(process.close)
		t.Cleanup(func() { _ = serverConn.Close() })
		return process, connection, serverConn
	}

	t.Run("success", func(t *testing.T) {
		process, connection, serverConn := newProcess(t)
		done := make(chan error, 1)
		go func() { done <- process.requestExit(t.Context()) }()

		input := make([]byte, len("exit\n"))
		_, err := io.ReadFull(serverConn, input)
		require.NoError(t, err)
		require.Equal(t, "exit\n", string(input))
		require.NoError(t, <-done)
		<-connection.inputClosed
	})

	t.Run("cancellation", func(t *testing.T) {
		process, connection, _ := newProcess(t)
		exitCtx, cancelExit := context.WithCancelCause(t.Context())
		exitErr := errors.New("cancel console exit")
		done := make(chan error, 1)
		go func() { done <- process.requestExit(exitCtx) }()
		<-connection.started
		cancelExit(exitErr)

		select {
		case err := <-done:
			require.ErrorIs(t, err, exitErr)
		case <-time.After(time.Second):
			t.Fatal("console exit request remained blocked after cancellation")
		}
		select {
		case <-connection.writeDone:
		case <-time.After(time.Second):
			t.Fatal("blocked console exit writer was not released")
		}
		select {
		case <-connection.inputClosed:
		case <-time.After(time.Second):
			t.Fatal("console input was not closed after cancellation")
		}
	})
}

func TestConsoleFixtureArchive(t *testing.T) {
	parameters := []byte(`{"chainID":"0x539"}`)
	fixtureArchive, err := consoleFixtureArchive(parameters)
	require.NoError(t, err)

	archive := tar.NewReader(bytes.NewReader(fixtureArchive))
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
	fixtureNames, err := fs.Glob(consoleFixtures, consoleFixtureDirectory+"/*.js")
	require.NoError(t, err)
	require.Len(t, contents, len(fixtureNames)+1)
	for _, name := range fixtureNames {
		expected, err := consoleFixtures.ReadFile(name)
		require.NoError(t, err)
		require.Equal(t, string(expected), contents[name])
	}
	require.Equal(
		t,
		"var PARAMS = "+string(parameters)+";\n",
		contents[consoleFixtureDirectory+"/.params.js"],
	)

	fixtureArchive, err = consoleFixtureArchive(nil)
	require.NoError(t, err)
	archive = tar.NewReader(bytes.NewReader(fixtureArchive))
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		require.NotEqual(t, consoleFixtureDirectory+"/.params.js", header.Name)
	}
}

func TestConsoleContainerEndpoint(t *testing.T) {
	for name, testCase := range map[string]struct {
		endpoint string
		want     string
		wantErr  bool
	}{
		"WebSocket": {
			endpoint: "ws://[::1]:8546",
			want:     "ws://host.docker.internal:8546",
		},
		"non-loopback": {
			endpoint: "https://rpc.example:443",
			want:     "https://host.docker.internal:443",
		},
		"missing port": {
			endpoint: "https://rpc.example",
			wantErr:  true,
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
	ctx                context.Context
	cancel             context.CancelFunc
	config             fakeConsoleProcessConfig
	closed             chan struct{}
	exit               chan struct{}
	exitOutput         chan struct{}
	outputStarted      chan struct{}
	outputDone         chan struct{}
	waitStarted        chan struct{}
	waitDone           chan struct{}
	exitRequestStarted chan struct{}
	closeOnce          sync.Once
	exitOnce           sync.Once
	exitRequestOnce    sync.Once
	exitRequests       atomic.Int32
}

type fakeConsoleProcessConfig struct {
	output     string
	exitOutput string
	readErr    error
	waitErr    error
	exitErr    error
	readGate   <-chan struct{}
	waitGate   <-chan struct{}
	exitGate   <-chan struct{}
	pending    bool
}

func newFakeConsoleProcess(ctx context.Context, config fakeConsoleProcessConfig) *fakeConsoleProcess {
	processCtx, cancel := context.WithCancel(ctx)
	return &fakeConsoleProcess{
		ctx: processCtx, cancel: cancel, config: config,
		closed:             make(chan struct{}),
		exit:               make(chan struct{}),
		exitOutput:         make(chan struct{}),
		outputStarted:      make(chan struct{}),
		outputDone:         make(chan struct{}),
		waitStarted:        make(chan struct{}),
		waitDone:           make(chan struct{}),
		exitRequestStarted: make(chan struct{}),
	}
}

func (process *fakeConsoleProcess) readOutput(destination io.Writer) error {
	close(process.outputStarted)
	defer close(process.outputDone)
	if process.config.readGate != nil {
		<-process.config.readGate
	}
	if _, err := io.WriteString(destination, process.config.output); err != nil {
		return err
	}
	if process.config.readErr != nil {
		return process.config.readErr
	}
	if process.config.exitOutput != "" {
		select {
		case <-process.exit:
			if _, err := io.WriteString(destination, process.config.exitOutput); err != nil {
				return err
			}
			close(process.exitOutput)
		case <-process.closed:
			return nil
		}
	}
	<-process.closed
	return nil
}

func (process *fakeConsoleProcess) wait() error {
	close(process.waitStarted)
	defer close(process.waitDone)
	if process.config.waitGate != nil {
		<-process.config.waitGate
	}
	if !process.config.pending {
		process.close()
		return process.config.waitErr
	}
	select {
	case <-process.exit:
		if process.config.exitOutput != "" {
			<-process.exitOutput
		}
		process.close()
		return process.config.waitErr
	case <-process.ctx.Done():
		process.close()
		return process.ctx.Err()
	}
}

func (process *fakeConsoleProcess) requestExit(ctx context.Context) error {
	process.exitRequests.Add(1)
	process.exitRequestOnce.Do(func() { close(process.exitRequestStarted) })
	if process.config.exitGate != nil {
		select {
		case <-process.config.exitGate:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
	if process.config.exitErr != nil {
		return process.config.exitErr
	}
	process.exitOnce.Do(func() { close(process.exit) })
	return nil
}

func (process *fakeConsoleProcess) close() {
	process.closeOnce.Do(func() {
		process.cancel()
		close(process.closed)
	})
}

func (process *fakeConsoleProcess) exitRequestCount() int {
	return int(process.exitRequests.Load())
}

func runFakeSuite(ctx context.Context, engine consoleContainerEngine) error {
	return runScenarioWithEngine(ctx, consoleContainerConfig{
		image:       "image",
		endpointURL: "http://127.0.0.1:8545",
		scenario:    "api",
	}, fakeFixtureArchive, engine)
}

func runFakeWatchedSuite(ctx context.Context, engine consoleContainerEngine) error {
	return runScenarioWithEngine(ctx, consoleContainerConfig{
		image:       "image",
		endpointURL: "ws://127.0.0.1:8546",
		scenario:    "events",
		interactive: true,
	}, fakeFixtureArchive, engine)
}

var fakeConsoleLifecycle = []string{
	"create",
	"copy:" + fakeContainerID,
	"start:" + fakeContainerID,
	"remove:" + fakeContainerID,
}

func TestRunSuite(t *testing.T) {
	engine := &fakeConsoleEngine{process: fakeConsoleProcessConfig{output: passPrefix + "api\n"}}
	err := runScenarioWithEngine(
		t.Context(),
		consoleContainerConfig{
			image:       "registry.example/go-qrl@sha256:digest",
			endpointURL: "http://127.0.0.1:8545",
			scenario:    "api",
		},
		fakeFixtureArchive,
		engine,
	)
	require.NoError(t, err)
	require.Equal(t, consoleContainerConfig{
		image:       "registry.example/go-qrl@sha256:digest",
		endpointURL: "http://127.0.0.1:8545",
		scenario:    "api",
	}, engine.config)
	require.Equal(t, fakeFixtureArchive, engine.fixtureArchive)
	require.False(t, engine.startInteractive)
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
		{name: "script", output: failPrefix + "api helper failure\n", wantDetail: "emitted a failure marker"},
		{name: "process", waitErr: processErr, wantErr: processErr},
		{name: "output", readErr: outputErr, pending: true, wantErr: outputErr},
		{name: "cleanup", output: passPrefix + "api\n", removeErr: cleanupErr, wantErr: cleanupErr},
		{
			name:       "script and cleanup",
			output:     failPrefix + "api helper failure\n",
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

func TestRunSuiteJoinsErrors(t *testing.T) {
	processErr := errors.New("process failed")
	outputErr := errors.New("output failed")
	for _, testCase := range []struct {
		name         string
		processFirst bool
	}{
		{name: "process first", processFirst: true},
		{name: "output first"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				readGate := make(chan struct{})
				waitGate := make(chan struct{})
				started := make(chan struct{})
				engine := &fakeConsoleEngine{process: fakeConsoleProcessConfig{
					output:   passPrefix + "api\n",
					readErr:  outputErr,
					waitErr:  processErr,
					readGate: readGate,
					waitGate: waitGate,
				}, onStart: func() { close(started) }}
				done := make(chan error, 1)
				go func() { done <- runFakeSuite(t.Context(), engine) }()
				<-started
				<-engine.startedProcess.outputStarted
				<-engine.startedProcess.waitStarted
				if testCase.processFirst {
					close(waitGate)
					<-engine.startedProcess.waitDone
					synctest.Wait()
					close(readGate)
				} else {
					close(readGate)
					<-engine.startedProcess.outputDone
					synctest.Wait()
					close(waitGate)
				}

				err := <-done
				require.ErrorIs(t, err, processErr)
				require.ErrorIs(t, err, outputErr)
				require.ErrorContains(t, err, passPrefix+"api")
			})
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

func TestRunCancellationWithBlockedOutput(t *testing.T) {
	for _, testCase := range []struct {
		name string
		run  func(context.Context, consoleContainerEngine) error
	}{
		{name: "suite", run: runFakeSuite},
		{name: "watched", run: runFakeWatchedSuite},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancelCause(t.Context())
				cancelErr := errors.New("cancel console run")
				readGate := make(chan struct{})
				started := make(chan struct{})
				engine := &fakeConsoleEngine{
					process: fakeConsoleProcessConfig{readGate: readGate, pending: true},
					onStart: func() { close(started) },
				}
				done := make(chan error, 1)
				go func() { done <- testCase.run(ctx, engine) }()
				<-started
				<-engine.startedProcess.outputStarted
				<-engine.startedProcess.waitStarted
				cancel(cancelErr)

				err := <-done
				require.ErrorIs(t, err, cancelErr)
				require.Equal(t, fakeConsoleLifecycle, engine.calls)
				require.NoError(t, engine.removeContextErr)
				close(readGate)
			})
		})
	}
}

func TestRunWatchedSuite(t *testing.T) {
	engine := &fakeConsoleEngine{process: fakeConsoleProcessConfig{
		output:  passPrefix + "events\n",
		pending: true,
	}}

	err := runFakeWatchedSuite(t.Context(), engine)
	require.NoError(t, err)
	require.True(t, engine.config.interactive)
	require.True(t, engine.startInteractive)
	require.Equal(t, 1, engine.startedProcess.exitRequestCount())
	require.Equal(t, fakeConsoleLifecycle, engine.calls)
}

func TestRunWatchedProcessAlreadyExited(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		readGate := make(chan struct{})
		started := make(chan struct{})
		engine := &fakeConsoleEngine{
			process: fakeConsoleProcessConfig{
				output:   passPrefix + "events\n",
				readGate: readGate,
			},
			onStart: func() { close(started) },
		}
		done := make(chan error, 1)
		go func() { done <- runFakeWatchedSuite(t.Context(), engine) }()
		<-started
		<-engine.startedProcess.outputStarted
		<-engine.startedProcess.waitDone
		synctest.Wait()
		close(readGate)

		require.NoError(t, <-done)
		require.Zero(t, engine.startedProcess.exitRequestCount())
	})
}

func TestRunShutdownTimeout(t *testing.T) {
	t.Run("exit request", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			exitGate := make(chan struct{})
			process := newFakeConsoleProcess(t.Context(), fakeConsoleProcessConfig{
				output:   passPrefix + "events\n",
				exitGate: exitGate,
				pending:  true,
			})
			started := time.Now()

			err := runConsoleProcess(t.Context(), process, "events", true)

			require.ErrorContains(t, err, "console process did not shut down within 5s")
			require.Equal(t, consoleProcessExitTimeout, time.Since(started))
			require.Equal(t, 1, process.exitRequestCount())
		})
	})

	t.Run("output", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			readGate := make(chan struct{})
			process := newFakeConsoleProcess(t.Context(), fakeConsoleProcessConfig{
				output:   passPrefix + "api\n",
				readGate: readGate,
			})
			started := time.Now()

			err := runConsoleProcess(t.Context(), process, "api", false)

			require.ErrorContains(t, err, "console process did not shut down within 5s")
			require.Equal(t, consoleProcessExitTimeout, time.Since(started))
			close(readGate)
		})
	})
}

func TestFinishConsoleProcessCancellation(t *testing.T) {
	shutdownErr := errors.New("shutdown timeout")
	err := finishConsoleProcess("events", consoleProcessResult{
		output: &consoleOutputResult{
			output: []byte(passPrefix + "events\n"),
		},
		processComplete: true,
		processErr:      context.Canceled,
		supervisorErr:   shutdownErr,
	})

	require.ErrorIs(t, err, shutdownErr)
	require.NotErrorIs(t, err, context.Canceled)
	require.NotContains(t, err.Error(), "run console suite events")
}

func TestOutputFailurePreventsExitRequest(t *testing.T) {
	terminal := make(chan struct{})
	close(terminal)
	process := newFakeConsoleProcess(t.Context(), fakeConsoleProcessConfig{})
	supervisor := &consoleProcessSupervisor{
		ctx:         t.Context(),
		process:     process,
		interactive: true,
		terminal:    terminal,
		processDone: make(chan error),
	}
	defer supervisor.cancel()

	supervisor.handleOutput(consoleOutputResult{
		terminalDetected: true,
		err:              errors.New("read failed"),
	})
	supervisor.requestExit()

	require.Zero(t, process.exitRequestCount())
}

func TestRunWatchedFailures(t *testing.T) {
	exitErr := errors.New("exit request failed")
	processErr := errors.New("process failed")
	for _, testCase := range []struct {
		name       string
		process    fakeConsoleProcessConfig
		wantErr    error
		wantDetail string
		wantExit   bool
	}{
		{
			name: "failure after success",
			process: fakeConsoleProcessConfig{
				output:     passPrefix + "events\n",
				exitOutput: failPrefix + "events helper failure\n",
				pending:    true,
			},
			wantDetail: "emitted a failure marker",
			wantExit:   true,
		},
		{
			name:       "GoError",
			process:    fakeConsoleProcessConfig{output: "GoError: helper failure\n", pending: true},
			wantDetail: "failed with GoError",
			wantExit:   true,
		},
		{
			name:       "exit request",
			process:    fakeConsoleProcessConfig{output: passPrefix + "events\n", exitErr: exitErr, pending: true},
			wantErr:    exitErr,
			wantDetail: "stop console suite events",
			wantExit:   true,
		},
		{
			name: "process after pass",
			process: fakeConsoleProcessConfig{
				output:  passPrefix + "events\n",
				waitErr: processErr,
				pending: true,
			},
			wantErr:  processErr,
			wantExit: true,
		},
		{
			name:       "early exit",
			process:    fakeConsoleProcessConfig{output: "console exited early\n"},
			wantDetail: "emitted 0 success markers",
		},
		{
			name:       "output",
			process:    fakeConsoleProcessConfig{readErr: errors.New("attach read failed"), pending: true},
			wantDetail: "attach read failed",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			engine := &fakeConsoleEngine{process: testCase.process}
			err := runFakeWatchedSuite(t.Context(), engine)
			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)
			}
			require.ErrorContains(t, err, testCase.wantDetail)
			if testCase.wantExit {
				require.Equal(t, 1, engine.startedProcess.exitRequestCount())
			}
			require.Equal(t, fakeConsoleLifecycle, engine.calls)
		})
	}
}

func TestRunWatchedBlockedExit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(t.Context())
		cancelErr := errors.New("cancel blocked exit")
		exitGate := make(chan struct{})
		started := make(chan struct{})
		engine := &fakeConsoleEngine{
			process: fakeConsoleProcessConfig{
				output:   passPrefix + "events\n",
				exitGate: exitGate,
				pending:  true,
			},
			onStart: func() { close(started) },
		}
		done := make(chan error, 1)
		go func() { done <- runFakeWatchedSuite(ctx, engine) }()
		<-started
		<-engine.startedProcess.exitRequestStarted
		cancel(cancelErr)

		err := <-done
		require.ErrorIs(t, err, cancelErr)
		require.Equal(t, 1, strings.Count(err.Error(), cancelErr.Error()))
		require.Equal(t, 1, engine.startedProcess.exitRequestCount())
		require.NoError(t, engine.removeContextErr)
		require.Equal(t, fakeConsoleLifecycle, engine.calls)
		close(exitGate)
	})
}
