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
	"time"

	"github.com/cyyber/qrl-tests/internal/dockerapi"
	"github.com/moby/moby/api/pkg/stdcopy"
	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
)

const (
	passPrefix = "CONSOLE_E2E_PASS "
	failPrefix = "CONSOLE_E2E_FAIL "

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
	ctx    context.Context
	cancel context.CancelFunc
	attach dockerclient.ContainerAttachResult
	waiter dockerclient.ContainerWaitResult
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
	process.cancel()
	process.attach.Close()
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

type terminalSignal uint8

const (
	terminalSignalNone terminalSignal = iota
	terminalSignalPass
	terminalSignalFail
	terminalSignalGoError
)

type terminalMarkers struct {
	pass             []byte
	fail             []byte
	failDetailPrefix []byte
}

func newTerminalMarkers(name string) terminalMarkers {
	return terminalMarkers{
		pass:             []byte(passPrefix + name),
		fail:             []byte(failPrefix + name),
		failDetailPrefix: []byte(failPrefix + name + " "),
	}
}

func (markers terminalMarkers) detect(line []byte) terminalSignal {
	line = bytes.TrimSpace(line)
	switch {
	case bytes.Equal(line, markers.pass):
		return terminalSignalPass
	case bytes.Equal(line, markers.fail), bytes.HasPrefix(line, markers.failDetailPrefix):
		return terminalSignalFail
	case bytes.Contains(line, []byte("GoError:")):
		return terminalSignalGoError
	default:
		return terminalSignalNone
	}
}

type consoleOutput struct {
	data             bytes.Buffer
	line             []byte
	terminal         chan struct{}
	terminalDetected bool
	markers          terminalMarkers
}

func newConsoleOutput(name string, watched bool) *consoleOutput {
	output := &consoleOutput{markers: newTerminalMarkers(name)}
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
	if output.terminalDetected || output.markers.detect(line) == terminalSignalNone {
		return
	}
	output.terminalDetected = true
	close(output.terminal)
}

func (output *consoleOutput) complete(err error) consoleOutputResult {
	if output.terminal != nil && len(output.line) > 0 {
		output.inspect(output.line)
	}
	return consoleOutputResult{
		output:           bytes.Clone(output.data.Bytes()),
		terminalDetected: output.terminalDetected,
		err:              err,
	}
}

type consoleOutputResult struct {
	output           []byte
	terminalDetected bool
	err              error
}

type consoleProcessResult struct {
	output          *consoleOutputResult
	processComplete bool
	forcedClose     bool
	processErr      error
	exitErr         error
	supervisorErr   error
}

type consoleProcessSupervisor struct {
	ctx         context.Context
	process     consoleContainerProcess
	name        string
	interactive bool

	terminal        <-chan struct{}
	outputDone      <-chan consoleOutputResult
	processDone     <-chan error
	exitRequestDone <-chan error

	shutdownCtx    context.Context
	shutdownDone   <-chan struct{}
	cancelShutdown context.CancelFunc
	result         consoleProcessResult
}

func runConsoleProcess(
	ctx context.Context,
	process consoleContainerProcess,
	name string,
	interactive bool,
) error {
	return newConsoleProcessSupervisor(ctx, process, name, interactive).run()
}

func newConsoleProcessSupervisor(
	ctx context.Context,
	process consoleContainerProcess,
	name string,
	interactive bool,
) *consoleProcessSupervisor {
	output := newConsoleOutput(name, interactive)
	outputDone := make(chan consoleOutputResult, 1)
	go func() {
		readErr := process.readOutput(output)
		outputDone <- output.complete(readErr)
	}()
	processDone := make(chan error, 1)
	go func() {
		processDone <- process.wait()
	}()

	return &consoleProcessSupervisor{
		ctx:         ctx,
		process:     process,
		name:        name,
		interactive: interactive,
		terminal:    output.terminal,
		outputDone:  outputDone,
		processDone: processDone,
	}
}

func (supervisor *consoleProcessSupervisor) run() error {
	defer supervisor.cancel()
	for supervisor.outputDone != nil || supervisor.processDone != nil {
		select {
		case output := <-supervisor.outputDone:
			supervisor.handleOutput(output)
		case err := <-supervisor.processDone:
			supervisor.handleProcess(err)
		case <-supervisor.terminal:
			supervisor.requestExit()
		case err := <-supervisor.exitRequestDone:
			supervisor.handleExitRequest(err)
		case <-supervisor.ctx.Done():
			return supervisor.abort(context.Cause(supervisor.ctx))
		case <-supervisor.shutdownDone:
			return supervisor.abort(context.Cause(supervisor.shutdownCtx))
		}
	}
	return supervisor.finish()
}

func (supervisor *consoleProcessSupervisor) handleOutput(output consoleOutputResult) {
	supervisor.outputDone = nil
	supervisor.result.output = &output
	switch {
	case output.err != nil:
		supervisor.forceClose()
	case supervisor.interactive && supervisor.processDone != nil && output.terminalDetected:
		supervisor.requestExit()
	case supervisor.interactive && supervisor.processDone != nil:
		supervisor.forceClose()
	}
	if supervisor.processDone != nil {
		supervisor.beginShutdown()
	}
}

func (supervisor *consoleProcessSupervisor) handleProcess(err error) {
	supervisor.processDone = nil
	supervisor.result.processComplete = true
	supervisor.result.processErr = err
	if supervisor.outputDone != nil {
		supervisor.beginShutdown()
	}
}

func (supervisor *consoleProcessSupervisor) handleExitRequest(err error) {
	supervisor.exitRequestDone = nil
	supervisor.result.exitErr = err
	if err != nil {
		supervisor.forceClose()
	}
}

func (supervisor *consoleProcessSupervisor) requestExit() {
	if supervisor.processDone == nil || supervisor.terminal == nil || supervisor.result.forcedClose {
		return
	}
	supervisor.terminal = nil
	select {
	case err := <-supervisor.processDone:
		supervisor.handleProcess(err)
		return
	default:
	}

	supervisor.beginShutdown()
	done := make(chan error, 1)
	supervisor.exitRequestDone = done
	go func() {
		done <- supervisor.process.requestExit(supervisor.shutdownCtx)
	}()
}

func (supervisor *consoleProcessSupervisor) beginShutdown() {
	if supervisor.shutdownCtx != nil {
		return
	}
	supervisor.shutdownCtx, supervisor.cancelShutdown = context.WithTimeoutCause(
		supervisor.ctx,
		consoleProcessExitTimeout,
		fmt.Errorf("console process did not shut down within %s", consoleProcessExitTimeout),
	)
	supervisor.shutdownDone = supervisor.shutdownCtx.Done()
}

func (supervisor *consoleProcessSupervisor) forceClose() {
	if supervisor.result.forcedClose {
		return
	}
	supervisor.terminal = nil
	supervisor.result.forcedClose = true
	supervisor.process.close()
}

func (supervisor *consoleProcessSupervisor) abort(err error) error {
	supervisor.forceClose()
	supervisor.result.supervisorErr = err
	return supervisor.finish()
}

func (supervisor *consoleProcessSupervisor) finish() error {
	supervisor.collectReady()
	if supervisor.result.supervisorErr == nil {
		switch {
		case context.Cause(supervisor.ctx) != nil:
			supervisor.result.supervisorErr = context.Cause(supervisor.ctx)
		case supervisor.shutdownCtx != nil && context.Cause(supervisor.shutdownCtx) != nil:
			supervisor.result.supervisorErr = context.Cause(supervisor.shutdownCtx)
		}
	}
	return finishConsoleProcess(supervisor.name, supervisor.result)
}

func (supervisor *consoleProcessSupervisor) collectReady() {
	if supervisor.result.output == nil && supervisor.outputDone != nil {
		select {
		case output := <-supervisor.outputDone:
			supervisor.outputDone = nil
			supervisor.result.output = &output
		default:
		}
	}
	if !supervisor.result.processComplete && supervisor.processDone != nil {
		select {
		case err := <-supervisor.processDone:
			supervisor.processDone = nil
			supervisor.result.processComplete = true
			supervisor.result.processErr = err
		default:
		}
	}
	if supervisor.exitRequestDone != nil {
		select {
		case err := <-supervisor.exitRequestDone:
			supervisor.exitRequestDone = nil
			supervisor.result.exitErr = err
		default:
		}
	}
}

func (supervisor *consoleProcessSupervisor) cancel() {
	if supervisor.cancelShutdown != nil {
		supervisor.cancelShutdown()
	}
}

func finishConsoleProcess(name string, result consoleProcessResult) error {
	naturalExit := result.processComplete && result.processErr == nil && !result.forcedClose
	processErr := result.processErr
	if (result.forcedClose || result.supervisorErr != nil) &&
		errors.Is(processErr, context.Canceled) {
		processErr = nil
	}
	exitErr := result.exitErr
	if naturalExit ||
		(result.supervisorErr != nil && errors.Is(exitErr, result.supervisorErr)) {
		exitErr = nil
	}

	var resultErr error
	if result.output != nil {
		resultErr = parseSuiteResult(name, result.output.output)
		if result.output.err != nil {
			resultErr = errors.Join(
				resultErr,
				fmt.Errorf("read console suite %s output: %w", name, result.output.err),
			)
		}
	}
	if processErr != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("run console suite %s: %w", name, processErr))
	}
	if exitErr != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("stop console suite %s: %w", name, exitErr))
	}
	if result.supervisorErr != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("console suite %s: %w", name, result.supervisorErr))
	}
	if resultErr == nil {
		return nil
	}
	if result.output == nil || len(result.output.output) == 0 {
		return resultErr
	}
	return fmt.Errorf("%w\n%s", resultErr, result.output.output)
}

func parseSuiteResult(name string, output []byte) error {
	markers := newTerminalMarkers(name)
	successes := 0
	failure, goError := false, false
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		switch markers.detect(line) {
		case terminalSignalPass:
			successes++
		case terminalSignalFail:
			failure = true
		case terminalSignalGoError:
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
