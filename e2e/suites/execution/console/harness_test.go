package console

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sync"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/gqrl"
)

const (
	passPrefix = "CONSOLE_E2E_PASS "
	failPrefix = "CONSOLE_E2E_FAIL "

	fixtureDirectory   = "testdata/console"
	processExitTimeout = 5 * time.Second
)

//go:embed testdata/console/*.js
var fixtures embed.FS

type consoleScenario struct {
	image       string
	endpointURL string
	scenario    string
	interactive bool
}

// consoleProcess is the part of *sidecar.Process the harness uses; tests replace
// it with a fake.
type consoleProcess interface {
	Output(io.Writer) error
	CloseInput(context.Context, string) error
	Wait() error
	Detach()
	Close() error
}

type startConsole func(context.Context, gqrl.Config) (consoleProcess, error)

func attachConsole(ctx context.Context, config gqrl.Config) (consoleProcess, error) {
	console, err := gqrl.Attach(ctx, config)
	if err != nil {
		return nil, err
	}
	return console, nil
}

// consoleConfig runs the harness, the assertions and the scenario's script.
func consoleConfig(config consoleScenario, scripts []sidecar.File) gqrl.Config {
	return gqrl.Config{
		Image:       config.image,
		EndpointURL: config.endpointURL,
		Scripts:     scripts,
		Run:         []string{"harness.js", "assertions.js", config.scenario + ".js"},
		Interactive: config.interactive,
	}
}

// consoleScripts returns the embedded console scripts, plus the scenario
// parameters as .params.js when there are any.
func consoleScripts(parameters []byte) ([]sidecar.File, error) {
	entries, err := fs.ReadDir(fixtures, fixtureDirectory)
	if err != nil {
		return nil, fmt.Errorf("read console fixtures: %w", err)
	}
	scripts := make([]sidecar.File, 0, len(entries)+1)
	for _, entry := range entries {
		body, err := fs.ReadFile(fixtures, path.Join(fixtureDirectory, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read console fixture %s: %w", entry.Name(), err)
		}
		scripts = append(scripts, sidecar.File{Name: entry.Name(), Body: body, Mode: 0o444})
	}

	if len(parameters) > 0 {
		parameterScript := fmt.Appendf(nil, "var PARAMS = %s;\n", parameters)
		scripts = append(scripts, sidecar.File{Name: ".params.js", Body: parameterScript, Mode: 0o444})
	}
	return scripts, nil
}

func runScenario(ctx context.Context, config consoleScenario, scripts []sidecar.File) error {
	return runScenarioWith(ctx, config, scripts, attachConsole)
}

func runScenarioWith(
	ctx context.Context,
	config consoleScenario,
	scripts []sidecar.File,
	start startConsole,
) (result error) {
	process, err := start(ctx, consoleConfig(config, scripts))
	if err != nil {
		return fmt.Errorf("start console suite %s: %w", config.scenario, err)
	}
	defer func() {
		if err := process.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("close console suite %s: %w", config.scenario, err))
		}
	}()
	return newProcessSupervisor(ctx, process, config.scenario, config.interactive).run()
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

type eventKind uint8

const (
	eventSignalDetected eventKind = iota
	eventOutputDone
	eventWaitDone
	eventExitRequestFailed
)

type processEvent struct {
	kind   eventKind
	output outputResult
	err    error
}

// outputRecorder keeps the console's output and, in interactive mode, reports
// the first result marker.
type outputRecorder struct {
	mu             sync.Mutex
	data           bytes.Buffer
	line           []byte
	events         chan<- processEvent
	watchForResult bool
	markers        terminalMarkers
}

func newOutputRecorder(
	name string,
	events chan<- processEvent,
	watchForResult bool,
) *outputRecorder {
	return &outputRecorder{
		events:         events,
		watchForResult: watchForResult,
		markers:        newTerminalMarkers(name),
	}
}

func (output *outputRecorder) Write(data []byte) (int, error) {
	output.mu.Lock()
	written, err := output.data.Write(data)
	output.mu.Unlock()
	if !output.watchForResult {
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

func (output *outputRecorder) inspect(line []byte) {
	if !output.watchForResult || output.markers.detect(line) == terminalSignalNone {
		return
	}
	output.watchForResult = false
	output.events <- processEvent{kind: eventSignalDetected}
}

func (output *outputRecorder) complete(readErr error) outputResult {
	if output.watchForResult && len(output.line) > 0 {
		output.inspect(output.line)
	}
	return outputResult{
		output:  output.snapshot(),
		readErr: readErr,
	}
}

// snapshot returns the output recorded so far.
func (output *outputRecorder) snapshot() []byte {
	output.mu.Lock()
	defer output.mu.Unlock()
	return bytes.Clone(output.data.Bytes())
}

type outputResult struct {
	output  []byte
	readErr error
}

type processResult struct {
	output                 *outputResult
	containerWaitCompleted bool
	forcedClose            bool
	containerWaitErr       error
	exitRequestErr         error
}

// processSupervisor waits for the console's output to end and its container
// to exit. In interactive mode it watches the output for the result marker,
// then sends exit on stdin. Once either finishes, the other gets
// processExitTimeout before the console is detached.
type processSupervisor struct {
	ctx         context.Context
	process     consoleProcess
	name        string
	interactive bool
	recorder    *outputRecorder

	events        chan processEvent
	exitRequested bool

	shutdownCtx    context.Context
	shutdownDone   <-chan struct{}
	shutdownCancel context.CancelFunc
	result         processResult
}

func newProcessSupervisor(
	ctx context.Context,
	process consoleProcess,
	name string,
	interactive bool,
) *processSupervisor {
	// Each event kind is emitted at most once. The buffer lets every final send
	// complete even if the supervisor returns early.
	events := make(chan processEvent, 4)
	// --exec scenarios exit on their own. Interactive preload scenarios watch
	// for a terminal result so the supervisor can request a graceful exit.
	output := newOutputRecorder(name, events, interactive)
	go func() {
		readErr := process.Output(output)
		events <- processEvent{
			kind:   eventOutputDone,
			output: output.complete(readErr),
		}
	}()
	go func() {
		waitErr := process.Wait()
		events <- processEvent{kind: eventWaitDone, err: waitErr}
	}()

	return &processSupervisor{
		ctx:         ctx,
		process:     process,
		name:        name,
		interactive: interactive,
		recorder:    output,
		events:      events,
	}
}

func (supervisor *processSupervisor) run() error {
	defer supervisor.cancelShutdownDeadline()
	for !supervisor.requiredResultsComplete() {
		select {
		case firstEvent := <-supervisor.events:
			terminalSignalDetected := supervisor.recordEventBatch(firstEvent)
			supervisor.respondToEvents(terminalSignalDetected)
		case <-supervisor.ctx.Done():
			return supervisor.abort(context.Cause(supervisor.ctx))
		case <-supervisor.shutdownDone:
			return supervisor.abort(context.Cause(supervisor.shutdownCtx))
		}
	}
	return supervisor.finish(nil)
}

func (supervisor *processSupervisor) requiredResultsComplete() bool {
	return supervisor.result.output != nil && supervisor.result.containerWaitCompleted
}

// recordEventBatch drains events already queued before responding, so each
// response uses all state currently available to the supervisor.
func (supervisor *processSupervisor) recordEventBatch(
	event processEvent,
) (terminalSignalDetected bool) {
	for {
		switch event.kind {
		case eventSignalDetected:
			terminalSignalDetected = true
		case eventOutputDone:
			output := event.output
			supervisor.result.output = &output
		case eventWaitDone:
			supervisor.result.containerWaitCompleted = true
			supervisor.result.containerWaitErr = event.err
		case eventExitRequestFailed:
			supervisor.result.exitRequestErr = event.err
		}
		select {
		case event = <-supervisor.events:
		default:
			return terminalSignalDetected
		}
	}
}

func (supervisor *processSupervisor) recordReadyEvents() {
	select {
	case firstEvent := <-supervisor.events:
		supervisor.recordEventBatch(firstEvent)
	default:
	}
}

func (supervisor *processSupervisor) respondToEvents(terminalSignalDetected bool) {
	if supervisor.requiredResultsComplete() {
		return
	}

	outputCompleted := supervisor.result.output != nil
	containerWaitCompleted := supervisor.result.containerWaitCompleted
	containerWaitSucceeded := containerWaitCompleted && supervisor.result.containerWaitErr == nil
	switch {
	case outputCompleted && supervisor.result.output.readErr != nil:
		supervisor.forceClose()
	case supervisor.result.exitRequestErr != nil && !containerWaitSucceeded:
		supervisor.forceClose()
	case supervisor.interactive && terminalSignalDetected:
		supervisor.requestExit()
	case supervisor.interactive && outputCompleted &&
		!containerWaitCompleted && !supervisor.exitRequested:
		supervisor.forceClose()
	}

	// Once either required result arrives, bound the wait for the other.
	if outputCompleted || containerWaitCompleted {
		supervisor.startShutdownDeadline()
	}
}

func (supervisor *processSupervisor) requestExit() {
	if supervisor.result.containerWaitCompleted || supervisor.exitRequested || supervisor.result.forcedClose {
		return
	}
	supervisor.exitRequested = true
	supervisor.startShutdownDeadline()
	go func() {
		if err := supervisor.process.CloseInput(supervisor.shutdownCtx, "exit\n"); err != nil {
			supervisor.events <- processEvent{
				kind: eventExitRequestFailed,
				err:  err,
			}
		}
	}()
}

func (supervisor *processSupervisor) startShutdownDeadline() {
	if supervisor.shutdownCtx != nil {
		return
	}
	supervisor.shutdownCtx, supervisor.shutdownCancel = context.WithTimeoutCause(
		supervisor.ctx,
		processExitTimeout,
		fmt.Errorf("console process did not shut down within %s", processExitTimeout),
	)
	supervisor.shutdownDone = supervisor.shutdownCtx.Done()
}

func (supervisor *processSupervisor) forceClose() {
	if supervisor.result.forcedClose {
		return
	}
	supervisor.result.forcedClose = true
	supervisor.process.Detach()
}

func (supervisor *processSupervisor) abort(err error) error {
	supervisor.forceClose()
	return supervisor.finish(err)
}

func (supervisor *processSupervisor) finish(supervisorErr error) error {
	supervisor.recordReadyEvents()
	// Cancellation or shutdown expiry can race the final completion event.
	if supervisorErr == nil {
		switch {
		case context.Cause(supervisor.ctx) != nil:
			supervisorErr = context.Cause(supervisor.ctx)
		case supervisor.shutdownCtx != nil && context.Cause(supervisor.shutdownCtx) != nil:
			supervisorErr = context.Cause(supervisor.shutdownCtx)
		}
	}
	err := finishProcess(supervisor.name, supervisor.result, supervisorErr)
	if err != nil && supervisor.result.output == nil {
		// The output never ended, so show what the console printed so far.
		if output := supervisor.recorder.snapshot(); len(output) > 0 {
			err = fmt.Errorf("%w\n%s", err, output)
		}
	}
	return err
}

func (supervisor *processSupervisor) cancelShutdownDeadline() {
	if supervisor.shutdownCancel != nil {
		supervisor.shutdownCancel()
	}
}

func finishProcess(name string, result processResult, supervisorErr error) error {
	exitedGracefully := result.containerWaitCompleted && result.containerWaitErr == nil && !result.forcedClose
	containerWaitErr := result.containerWaitErr
	if (result.forcedClose || supervisorErr != nil) &&
		(errors.Is(containerWaitErr, context.Canceled) || errors.Is(containerWaitErr, supervisorErr)) {
		containerWaitErr = nil
	}
	exitRequestErr := result.exitRequestErr
	if exitedGracefully ||
		(supervisorErr != nil && errors.Is(exitRequestErr, supervisorErr)) {
		exitRequestErr = nil
	}

	var resultErr error
	if result.output != nil {
		resultErr = parseSuiteResult(name, result.output.output)
		if result.output.readErr != nil {
			resultErr = errors.Join(
				resultErr,
				fmt.Errorf("read console suite %s output: %w", name, result.output.readErr),
			)
		}
	}
	if containerWaitErr != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("run console suite %s: %w", name, containerWaitErr))
	}
	if exitRequestErr != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("stop console suite %s: %w", name, exitRequestErr))
	}
	if supervisorErr != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("console suite %s: %w", name, supervisorErr))
	}
	if resultErr != nil && result.output != nil && len(result.output.output) > 0 {
		return fmt.Errorf("%w\n%s", resultErr, result.output.output)
	}
	return resultErr
}

func parseSuiteResult(name string, output []byte) error {
	markers := newTerminalMarkers(name)
	successes := 0
	goError := false
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		switch markers.detect(line) {
		case terminalSignalPass:
			successes++
		case terminalSignalFail:
			return fmt.Errorf("console suite %s emitted a failure marker", name)
		case terminalSignalGoError:
			goError = true
		}
	}
	if goError {
		return fmt.Errorf("console suite %s failed with GoError", name)
	}
	if successes != 1 {
		return fmt.Errorf("console suite %s emitted %d success markers", name, successes)
	}
	return nil
}
