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
	TestsDir     string
	BaseName     string
	ReportDir    string
	Backend      devnet.Backend
	Images       devnet.Images
	Parameters   []byte
	Suites       []string
	StartTimeout time.Duration
	MaxParallel  int
}

type networkManager interface {
	Start(context.Context, devnet.StartOptions) (devnet.Environment, error)
	Inspect(context.Context, string, devnet.Backend) (devnet.Environment, error)
	Stop(context.Context, string) error
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
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
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
	return command.Run()
}

func (runner *Runner) List() error {
	for _, lane := range lanes.All() {
		if _, err := fmt.Fprintf(
			runner.stdout,
			"%-16s profile=%-16s timeout=%-8s suites=%s\n",
			lane.Name,
			lane.Profile,
			lane.Timeout,
			suiteIDs(lane.Suites),
		); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(runner.stdout, "\nRegistered suites:"); err != nil {
		return err
	}
	for _, id := range lanes.RegisteredSuites() {
		if _, err := fmt.Fprintf(
			runner.stdout,
			"%-24s package=%s\n",
			id,
			id.Package(),
		); err != nil {
			return err
		}
	}
	return nil
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
	return lane.Select(runner.configuration.Suites)
}

func (runner *Runner) run(ctx context.Context, selected []lanes.Lane, mode runMode) error {
	plan, err := newRunPlan(runner.configuration, selected, mode)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(plan.reportRoot, 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	return runner.runLanes(ctx, plan.lanes)
}

func (runner *Runner) runLanes(ctx context.Context, planned []laneRun) error {
	limit := runner.configuration.MaxParallel
	if limit < 2 || len(planned) < 2 {
		var result error
		for _, lane := range planned {
			result = errors.Join(result, runner.runLane(ctx, lane))
		}
		return result
	}

	if limit > len(planned) {
		limit = len(planned)
	}
	semaphore := make(chan struct{}, limit)
	results := make([]error, len(planned))
	var group sync.WaitGroup
	for index, lane := range planned {
		group.Add(1)
		go func() {
			defer group.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results[index] = ctx.Err()
				return
			}
			results[index] = runner.runLane(ctx, lane)
		}()
	}
	group.Wait()

	var result error
	for _, err := range results {
		result = errors.Join(result, err)
	}
	return result
}

func ginkgoArguments(lane lanes.Lane, reportDir string) []string {
	arguments := []string{
		"tool", "ginkgo",
		"--tags=e2e",
		"--procs=1",
		"--keep-going",
		"--require-suite",
		"--fail-on-empty",
		"--fail-on-pending",
		"--timeout=" + lane.Timeout.String(),
		"--output-dir=" + reportDir,
		"--junit-report=junit.xml",
		"--json-report=report.json",
	}
	arguments = append(arguments, lane.Packages()...)
	return append(arguments, "--", "-test.run=^TestE2E$")
}
