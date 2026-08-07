// Package runner executes the registered end-to-end test lanes.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/lanes"
)

const DefaultReportDir = "reports"

type Config struct {
	TestsDir       string
	BaseName       string
	ReportDir      string
	Backend        devnet.Backend
	Images         devnet.Images
	PackageLocator string
	Parameters     []byte
	Suites         []string
	StartTimeout   time.Duration
	MaxParallel    int
}

type networkManager interface {
	Start(ctx context.Context, options devnet.StartOptions) (devnet.Environment, error)
	Inspect(ctx context.Context, name string) (devnet.Environment, error)
	Stop(ctx context.Context, name string) error
}

type commandSpec struct {
	Path   string
	Args   []string
	Dir    string
	Env    []string
	Stdout io.Writer
	Stderr io.Writer
}

type runMode uint8

const (
	useExistingNetwork runMode = iota
	provisionNetwork
	provisionPerLane
)

func (mode runMode) provisions() bool {
	return mode != useExistingNetwork
}

func (mode runMode) suffixesEnclave() bool {
	return mode == provisionPerLane
}

type Runner struct {
	configuration Config
	networks      networkManager
	runCommand    func(context.Context, commandSpec) error
	stdout        io.Writer
	stderr        io.Writer
}

func New(configuration Config, stdout, stderr io.Writer) *Runner {
	outputLock := new(sync.Mutex)
	return &Runner{
		configuration: configuration,
		networks:      devnet.NewManager(),
		runCommand:    execute,
		stdout:        &lockedWriter{lock: outputLock, writer: stdout},
		stderr:        &lockedWriter{lock: outputLock, writer: stderr},
	}
}

func execute(ctx context.Context, specification commandSpec) error {
	command := exec.CommandContext(ctx, specification.Path, specification.Args...)
	command.Dir = specification.Dir
	command.Env = specification.Env
	command.Stdout = specification.Stdout
	command.Stderr = specification.Stderr
	// Cancellation interrupts ginkgo so it can abort specs and still write its
	// reports. WaitDelay bounds that shutdown — and any test-binary children
	// surviving it while holding the output pipes — before the process is
	// killed and the pipes are force-closed.
	command.Cancel = func() error { return command.Process.Signal(os.Interrupt) }
	command.WaitDelay = 30 * time.Second
	return command.Run()
}

type lockedWriter struct {
	lock   *sync.Mutex
	writer io.Writer
}

func (writer *lockedWriter) Write(payload []byte) (int, error) {
	writer.lock.Lock()
	defer writer.lock.Unlock()
	return writer.writer.Write(payload)
}

func (runner *Runner) List() error {
	var listing strings.Builder
	for _, lane := range lanes.All() {
		fmt.Fprintf(&listing, "%-16s profile=%-16s timeout=%-8s suites=%s\n",
			lane.Name, lane.Profile, lane.Timeout, suiteIDs(lane.Suites))
	}

	listing.WriteString("\nRegistered suites:\n")
	for _, id := range lanes.RegisteredSuites() {
		fmt.Fprintf(&listing, "%-24s package=%s\n", id, id.Package())
	}

	_, err := fmt.Fprint(runner.stdout, listing.String())
	return err
}

func suiteIDs(ids []lanes.SuiteID) string {
	values := make([]string, len(ids))
	for index, id := range ids {
		values[index] = string(id)
	}
	return strings.Join(values, ",")
}

func (runner *Runner) Test(ctx context.Context, name string) error {
	if len(runner.configuration.Parameters) != 0 {
		return errors.New("custom parameters cannot be used with an existing network")
	}
	lane, err := runner.selectedLane(name)
	if err != nil {
		return err
	}
	return runner.run(ctx, []lanes.Lane{lane}, useExistingNetwork)
}

func (runner *Runner) Run(ctx context.Context, name string) error {
	lane, err := runner.selectedLane(name)
	if err != nil {
		return err
	}
	return runner.run(ctx, []lanes.Lane{lane}, provisionNetwork)
}

func (runner *Runner) RunAll(ctx context.Context) error {
	if len(runner.configuration.Parameters) != 0 {
		return errors.New("custom parameters cannot be used with run-all")
	}
	if len(runner.configuration.Suites) != 0 {
		return errors.New("suite selection cannot be used with run-all")
	}
	return runner.run(ctx, lanes.All(), provisionPerLane)
}

func (runner *Runner) selectedLane(name string) (lanes.Lane, error) {
	lane, err := lanes.Named(name)
	if err != nil {
		return lanes.Lane{}, err
	}
	return lane.WithSuites(runner.configuration.Suites)
}

func (runner *Runner) run(ctx context.Context, selected []lanes.Lane, mode runMode) error {
	planned, err := planLanes(runner.configuration, selected, mode)
	if err != nil {
		return err
	}
	return runner.runLanes(ctx, planned)
}

func (runner *Runner) runLanes(ctx context.Context, planned []laneRun) error {
	limit := runner.configuration.MaxParallel
	results := make([]error, len(planned))

	if limit < 2 || len(planned) < 2 {
		for index, lane := range planned {
			results[index] = runner.runLane(ctx, lane)
		}
		return errors.Join(results...)
	}

	semaphore := make(chan struct{}, limit)
	var group sync.WaitGroup
	for index, lane := range planned {
		group.Go(func() {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			results[index] = runner.runLane(ctx, lane)
		})
	}
	group.Wait()

	return errors.Join(results...)
}
