package console

import (
	"archive/tar"
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/cyyber/qrl-tests/internal/dockerapi"
	"github.com/moby/moby/api/pkg/stdcopy"
	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
)

const (
	resultPrefix  = "CONSOLE_E2E_PASS "
	failurePrefix = "CONSOLE_E2E_FAIL "

	consoleFixtureDirectory        = "testdata/console"
	consoleContainerRoot           = "/tmp"
	consoleContainerJSPath         = consoleContainerRoot + "/" + consoleFixtureDirectory
	consoleContainerDataDir        = consoleContainerRoot + "/qrl-tests-console"
	consoleContainerHost           = "host.docker.internal"
	consoleContainerCleanupTimeout = 30 * time.Second
	consoleProcessExitTimeout      = 5 * time.Second
)

//go:embed testdata/console/*.js
var consoleFixtures embed.FS

type consoleContainerConfig struct {
	image       string
	endpointURL string
	scenario    string
	interactive bool
}

type consoleContainerEngine interface {
	createContainer(context.Context, consoleContainerConfig) (string, error)
	copyFixtures(context.Context, string, []byte) error
	startContainer(context.Context, string, bool) (consoleContainerProcess, error)
	removeContainer(context.Context, string) error
}

type consoleContainerProcess interface {
	readOutput(io.Writer) error
	requestExit(context.Context) error
	wait() error
	close()
}

type consoleDockerClient interface {
	ContainerAttach(context.Context, string, dockerclient.ContainerAttachOptions) (dockerclient.ContainerAttachResult, error)
	ContainerCreate(context.Context, dockerclient.ContainerCreateOptions) (dockerclient.ContainerCreateResult, error)
	ContainerRemove(context.Context, string, dockerclient.ContainerRemoveOptions) (dockerclient.ContainerRemoveResult, error)
	ContainerStart(context.Context, string, dockerclient.ContainerStartOptions) (dockerclient.ContainerStartResult, error)
	ContainerWait(context.Context, string, dockerclient.ContainerWaitOptions) dockerclient.ContainerWaitResult
	CopyToContainer(context.Context, string, dockerclient.CopyToContainerOptions) (dockerclient.CopyToContainerResult, error)
}

type dockerConsoleEngine struct {
	client consoleDockerClient
}

func (engine dockerConsoleEngine) createContainer(ctx context.Context, config consoleContainerConfig) (string, error) {
	endpoint, err := consoleContainerEndpoint(config.endpointURL)
	if err != nil {
		return "", fmt.Errorf("create console suite %s container: %w", config.scenario, err)
	}

	arguments := []string{
		"attach",
		"--datadir", consoleContainerDataDir,
		"--jspath", consoleContainerJSPath,
	}
	if config.interactive {
		arguments = append(arguments, "--preload", "harness.js,assertions.js,"+config.scenario+".js")
	} else {
		arguments = append(
			arguments,
			"--exec",
			"loadScript('harness.js');loadScript('assertions.js');loadScript('"+config.scenario+".js')",
		)
	}
	arguments = append(arguments, endpoint)

	created, err := engine.client.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
		Config: &containertypes.Config{
			Image:        config.image,
			Entrypoint:   []string{"gqrl"},
			Cmd:          arguments,
			AttachStdin:  config.interactive,
			AttachStdout: true,
			AttachStderr: true,
			OpenStdin:    config.interactive,
			StdinOnce:    config.interactive,
		},
		HostConfig: &containertypes.HostConfig{
			ExtraHosts: []string{consoleContainerHost + ":host-gateway"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("create console suite %s container: %w", config.scenario, err)
	}
	if created.ID == "" {
		return "", fmt.Errorf("create console suite %s container: Docker returned no container ID", config.scenario)
	}
	return created.ID, nil
}

func (engine dockerConsoleEngine) copyFixtures(ctx context.Context, containerID string, archive []byte) error {
	if _, err := engine.client.CopyToContainer(ctx, containerID, dockerclient.CopyToContainerOptions{
		DestinationPath: consoleContainerRoot,
		Content:         bytes.NewReader(archive),
	}); err != nil {
		return fmt.Errorf("copy fixtures into console container: %w", err)
	}
	return nil
}

func (engine dockerConsoleEngine) startContainer(
	ctx context.Context,
	containerID string,
	interactive bool,
) (consoleContainerProcess, error) {
	processCtx, cancel := context.WithCancel(ctx)
	attached, err := engine.client.ContainerAttach(processCtx, containerID, dockerclient.ContainerAttachOptions{
		Stream: true,
		Stdin:  interactive,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("attach to console container: %w", err)
	}
	waiter := engine.client.ContainerWait(processCtx, containerID, dockerclient.ContainerWaitOptions{
		Condition: containertypes.WaitConditionNextExit,
	})
	select {
	case err := <-waiter.Error:
		if err != nil {
			cancel()
			attached.Close()
			return nil, fmt.Errorf("register console container exit waiter: %w", err)
		}
	default:
	}
	if _, err := engine.client.ContainerStart(processCtx, containerID, dockerclient.ContainerStartOptions{}); err != nil {
		cancel()
		attached.Close()
		return nil, fmt.Errorf("start console container: %w", err)
	}
	return &dockerConsoleProcess{
		ctx:    processCtx,
		cancel: cancel,
		attach: attached,
		waiter: waiter,
	}, nil
}

func (engine dockerConsoleEngine) removeContainer(ctx context.Context, containerID string) error {
	if _, err := engine.client.ContainerRemove(ctx, containerID, dockerclient.ContainerRemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("remove Docker container: %w", err)
	}
	return nil
}

type dockerConsoleProcess struct {
	ctx       context.Context
	cancel    context.CancelFunc
	attach    dockerclient.ContainerAttachResult
	waiter    dockerclient.ContainerWaitResult
	closeOnce sync.Once
}

func (process *dockerConsoleProcess) readOutput(destination io.Writer) error {
	_, err := stdcopy.StdCopy(destination, destination, process.attach.Reader)
	return err
}

func (process *dockerConsoleProcess) requestExit(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		_, writeErr := io.WriteString(process.attach.Conn, "exit\n")
		if writeErr != nil {
			writeErr = fmt.Errorf("write console exit command: %w", writeErr)
		}
		closeErr := process.attach.CloseWrite()
		if closeErr != nil {
			closeErr = fmt.Errorf("close console process input: %w", closeErr)
		}
		done <- errors.Join(writeErr, closeErr)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		process.close()
		return context.Cause(ctx)
	}
}

func (process *dockerConsoleProcess) wait() error {
	select {
	case response := <-process.waiter.Result:
		if response.Error != nil {
			return errors.New(response.Error.Message)
		}
		if response.StatusCode != 0 {
			return fmt.Errorf("exit status %d", response.StatusCode)
		}
		return nil
	case err := <-process.waiter.Error:
		return err
	case <-process.ctx.Done():
		return process.ctx.Err()
	}
}

func (process *dockerConsoleProcess) close() {
	process.closeOnce.Do(func() {
		process.cancel()
		process.attach.Close()
	})
}

func consoleContainerEndpoint(endpointURL string) (string, error) {
	endpoint, err := url.Parse(endpointURL)
	if err != nil {
		return "", fmt.Errorf("parse console endpoint: %w", err)
	}
	port := endpoint.Port()
	if endpoint.Scheme == "" || endpoint.Hostname() == "" || port == "" {
		return "", errors.New("parse console endpoint: URL must include a scheme, host, and port")
	}

	endpoint.Host = net.JoinHostPort(consoleContainerHost, port)
	return endpoint.String(), nil
}

func consoleFixtureArchive(parameters []byte) ([]byte, error) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.AddFS(consoleFixtures); err != nil {
		return nil, fmt.Errorf("archive console fixtures: %w", err)
	}

	if len(parameters) > 0 {
		parameterScript := append([]byte("var PARAMS = "), parameters...)
		parameterScript = append(parameterScript, ';', '\n')
		if err := writer.WriteHeader(&tar.Header{
			Name:     consoleFixtureDirectory + "/.params.js",
			Mode:     0o600,
			Size:     int64(len(parameterScript)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			return nil, fmt.Errorf("archive console parameters: %w", err)
		}
		if _, err := writer.Write(parameterScript); err != nil {
			return nil, fmt.Errorf("archive console parameters: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("archive console fixtures: %w", err)
	}
	return archive.Bytes(), nil
}

func runSuite(ctx context.Context, image, endpointURL, name string, fixtureArchive []byte) error {
	return runScenario(ctx, consoleContainerConfig{
		image:       image,
		endpointURL: endpointURL,
		scenario:    name,
	}, fixtureArchive)
}

func runScenario(
	ctx context.Context,
	config consoleContainerConfig,
	fixtureArchive []byte,
) error {
	client, err := dockerapi.New()
	if err != nil {
		return fmt.Errorf("create Docker client: %w", err)
	}
	defer func() { _ = client.Close() }()
	return runScenarioWithEngine(
		ctx,
		config,
		fixtureArchive,
		dockerConsoleEngine{client: client},
	)
}

func runScenarioWithEngine(
	ctx context.Context,
	config consoleContainerConfig,
	fixtureArchive []byte,
	engine consoleContainerEngine,
) (result error) {
	containerID, err := engine.createContainer(ctx, config)
	if err != nil {
		return err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), consoleContainerCleanupTimeout)
		defer cancel()
		if err := engine.removeContainer(cleanupCtx, containerID); err != nil {
			result = errors.Join(result, fmt.Errorf("remove console suite %s container: %w", config.scenario, err))
		}
	}()
	if err := engine.copyFixtures(ctx, containerID, fixtureArchive); err != nil {
		return fmt.Errorf("copy console suite %s fixtures: %w", config.scenario, err)
	}

	process, err := engine.startContainer(ctx, containerID, config.interactive)
	if err != nil {
		return fmt.Errorf("start console suite %s: %w", config.scenario, err)
	}
	defer process.close()
	return runConsoleProcess(ctx, process, config.scenario, config.interactive)
}

type consoleOutputResult struct {
	output []byte
	err    error
}

type suiteMarkerKind uint8

const (
	suiteMarkerNone suiteMarkerKind = iota
	suiteMarkerSuccess
	suiteMarkerFailure
	suiteMarkerGoError
)

type suiteResultMarkers struct {
	success       []byte
	failure       []byte
	failureDetail []byte
}

func newSuiteResultMarkers(name string) suiteResultMarkers {
	return suiteResultMarkers{
		success:       []byte(resultPrefix + name),
		failure:       []byte(failurePrefix + name),
		failureDetail: []byte(failurePrefix + name + " "),
	}
}

func (markers suiteResultMarkers) classify(line []byte) suiteMarkerKind {
	line = bytes.TrimSpace(line)
	switch {
	case bytes.Equal(line, markers.success):
		return suiteMarkerSuccess
	case bytes.Equal(line, markers.failure), bytes.HasPrefix(line, markers.failureDetail):
		return suiteMarkerFailure
	case bytes.Contains(line, []byte("GoError:")):
		return suiteMarkerGoError
	default:
		return suiteMarkerNone
	}
}

type consoleOutput struct {
	data         bytes.Buffer
	line         []byte
	terminal     chan struct{}
	terminalOnce sync.Once
	markers      suiteResultMarkers
}

func newConsoleOutput(name string, watched bool) *consoleOutput {
	output := &consoleOutput{markers: newSuiteResultMarkers(name)}
	if watched {
		output.terminal = make(chan struct{})
	}
	return output
}

func (output *consoleOutput) Write(data []byte) (int, error) {
	written, err := output.data.Write(data)
	if output.terminal == nil {
		return written, err
	}
	output.line = append(output.line, data[:written]...)
	for {
		end := bytes.IndexByte(output.line, '\n')
		if end < 0 {
			break
		}
		output.inspect(output.line[:end])
		output.line = output.line[end+1:]
	}
	return written, err
}

func (output *consoleOutput) inspect(line []byte) {
	if output.markers.classify(line) == suiteMarkerNone {
		return
	}
	output.terminalOnce.Do(func() { close(output.terminal) })
}

func (output *consoleOutput) complete(err error) consoleOutputResult {
	if output.terminal != nil && len(output.line) > 0 {
		output.inspect(output.line)
	}
	return consoleOutputResult{output: bytes.Clone(output.data.Bytes()), err: err}
}

func (output *consoleOutput) terminalSignal() <-chan struct{} {
	return output.terminal
}

type consoleProcessCompletionKind uint8

const (
	consoleOutputComplete consoleProcessCompletionKind = iota
	consoleProcessComplete
	consoleExitRequestComplete
)

type consoleProcessCompletion struct {
	kind   consoleProcessCompletionKind
	output consoleOutputResult
	err    error
}

type consoleProcessState struct {
	output          consoleOutputResult
	outputComplete  bool
	processComplete bool
	exitRequested   bool
	terminalSeen    bool
	forcedClose     bool
	processErr      error
	exitErr         error
	supervisorErr   error
}

func (state *consoleProcessState) record(completion consoleProcessCompletion) {
	switch completion.kind {
	case consoleOutputComplete:
		state.output = completion.output
		state.outputComplete = true
	case consoleProcessComplete:
		state.processErr = completion.err
		state.processComplete = true
	case consoleExitRequestComplete:
		state.exitErr = completion.err
	}
}

func (state *consoleProcessState) drain(completions <-chan consoleProcessCompletion) {
	for {
		select {
		case completion := <-completions:
			state.record(completion)
		default:
			return
		}
	}
}

func (state consoleProcessState) complete() bool {
	return state.outputComplete && state.processComplete
}

func runConsoleProcess(
	ctx context.Context,
	process consoleContainerProcess,
	name string,
	interactive bool,
) error {
	output := newConsoleOutput(name, interactive)
	completions := make(chan consoleProcessCompletion, 3)
	go func() {
		completions <- consoleProcessCompletion{
			kind:   consoleOutputComplete,
			output: output.complete(process.readOutput(output)),
		}
	}()
	go func() {
		completions <- consoleProcessCompletion{kind: consoleProcessComplete, err: process.wait()}
	}()

	terminal := output.terminalSignal()
	state := consoleProcessState{}
	var shutdownCtx context.Context
	var shutdownDone <-chan struct{}
	cancelShutdown := func() {}
	defer func() { cancelShutdown() }()

	closeProcess := func() {
		state.forcedClose = true
		process.close()
	}
	startShutdown := func() {
		if shutdownCtx == nil {
			shutdownCtx, cancelShutdown = context.WithTimeoutCause(
				ctx,
				consoleProcessExitTimeout,
				fmt.Errorf("console process did not shut down within %s", consoleProcessExitTimeout),
			)
			shutdownDone = shutdownCtx.Done()
		}
	}
	requestExit := func() {
		if state.processComplete || state.exitRequested {
			return
		}
		state.exitRequested = true
		startShutdown()
		go func() {
			completions <- consoleProcessCompletion{
				kind: consoleExitRequestComplete,
				err:  process.requestExit(shutdownCtx),
			}
		}()
	}
	consumeTerminal := func() {
		if terminal == nil {
			return
		}
		select {
		case <-terminal:
			terminal = nil
			state.terminalSeen = true
		default:
		}
	}
	reconcile := func() {
		consumeTerminal()
		if state.outputComplete && state.output.err != nil {
			closeProcess()
		}
		if state.exitErr != nil {
			closeProcess()
		}
		if interactive && !state.processComplete {
			switch {
			case state.terminalSeen:
				requestExit()
			case state.outputComplete:
				closeProcess()
			}
		}
		if !state.complete() &&
			(state.outputComplete || state.processComplete || state.exitRequested) {
			startShutdown()
		}
	}

	for {
		state.drain(completions)
		reconcile()
		if state.complete() {
			switch {
			case context.Cause(ctx) != nil:
				state.supervisorErr = context.Cause(ctx)
			case shutdownCtx != nil && context.Cause(shutdownCtx) != nil:
				state.supervisorErr = context.Cause(shutdownCtx)
			}
			return finishConsoleProcess(name, state)
		}

		select {
		case completion := <-completions:
			state.record(completion)
		case <-terminal:
			terminal = nil
			state.terminalSeen = true
		case <-ctx.Done():
			closeProcess()
			state.supervisorErr = context.Cause(ctx)
			state.drain(completions)
			return finishConsoleProcess(name, state)
		case <-shutdownDone:
			closeProcess()
			state.supervisorErr = context.Cause(shutdownCtx)
			state.drain(completions)
			return finishConsoleProcess(name, state)
		}
	}
}

func finishConsoleProcess(name string, state consoleProcessState) error {
	naturalExit := state.processComplete && state.processErr == nil && !state.forcedClose
	processErr := state.processErr
	if (state.forcedClose || state.supervisorErr != nil) &&
		errors.Is(processErr, context.Canceled) {
		processErr = nil
	}
	exitErr := state.exitErr
	if naturalExit ||
		(state.supervisorErr != nil && errors.Is(exitErr, state.supervisorErr)) {
		exitErr = nil
	}

	var resultErr error
	if state.outputComplete {
		resultErr = parseSuiteResult(name, state.output.output)
		if state.output.err != nil {
			resultErr = errors.Join(
				resultErr,
				fmt.Errorf("read console suite %s output: %w", name, state.output.err),
			)
		}
	}
	if processErr != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("run console suite %s: %w", name, processErr))
	}
	if exitErr != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("stop console suite %s: %w", name, exitErr))
	}
	if state.supervisorErr != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("console suite %s: %w", name, state.supervisorErr))
	}
	if resultErr == nil {
		return nil
	}
	if len(state.output.output) == 0 {
		return resultErr
	}
	return fmt.Errorf("%w\n%s", resultErr, state.output.output)
}

func parseSuiteResult(name string, output []byte) error {
	markers := newSuiteResultMarkers(name)
	successes := 0
	failure, goError := false, false
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		switch markers.classify(line) {
		case suiteMarkerSuccess:
			successes++
		case suiteMarkerFailure:
			failure = true
		case suiteMarkerGoError:
			goError = true
		}
	}
	if failure {
		return fmt.Errorf("console suite %s emitted a failure marker", name)
	}
	if goError {
		return fmt.Errorf("console suite %s failed with GoError", name)
	}
	if successes != 1 {
		return fmt.Errorf("console suite %s emitted %d success markers", name, successes)
	}
	return nil
}
