// Package runner executes the registered end-to-end test lanes.
package runner

import (
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/lanes"
	"github.com/cyyber/qrl-tests/internal/results"
	"github.com/cyyber/qrl-tests/internal/runmanifest"
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
	// Seed fixes ginkgo's spec ordering for every lane; zero draws a fresh
	// seed per lane, and the run manifest records whichever was used.
	Seed int64
}

func (configuration Config) withDefaults() Config {
	configuration.TestsDir = cmp.Or(configuration.TestsDir, ".")
	configuration.BaseName = cmp.Or(configuration.BaseName, devnet.DefaultEnclaveName)
	configuration.ReportDir = cmp.Or(configuration.ReportDir, DefaultReportDir)
	configuration.Backend = cmp.Or(configuration.Backend, devnet.BackendDocker)
	configuration.StartTimeout = cmp.Or(configuration.StartTimeout, devnet.DefaultStartTimeout)
	return configuration
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

func (mode runMode) provisionsNetwork() bool {
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
		configuration: configuration.withDefaults(),
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
	plan, err := planLanes(runner.configuration, selected, mode)
	if err != nil {
		return err
	}
	if err := clearReportArtifacts(plan.reportRoot); err != nil {
		return err
	}

	record := runner.collectManifest(ctx, plan)
	manifestPath := filepath.Join(plan.reportRoot, runmanifest.FileName)
	// The starting snapshot survives even a run the harness cannot finish.
	manifestErr := record.Write(manifestPath)

	outcomes := runner.runLanes(ctx, plan)
	summary, summarizeErr := results.Summarize(plan.reportRoot, outcomes)
	laneErrors := make([]error, len(outcomes))
	for index, outcome := range outcomes {
		laneErrors[index] = outcome.Err
	}

	// The summary is the verdict authority: a lane whose process exited
	// cleanly but whose report is missing or unusable is a failure, in the
	// manifest and in the exit code alike.
	laneResults := make(map[string]bool, len(summary.Lanes))
	for _, lane := range summary.Lanes {
		laneResults[lane.Name] = lane.Class == results.ClassPassed
	}
	record.Finish(laneResults, time.Now())
	if manifestErr != nil || summarizeErr != nil {
		record.Result = "failed"
	}
	manifestErr = errors.Join(manifestErr, record.Write(manifestPath))

	// Reporting problems never mask the test result, and vice versa. Preserve
	// raw lane errors as well so callers can inspect cancellation and timeout.
	return errors.Join(summary.VerdictError(), errors.Join(laneErrors...), manifestErr, summarizeErr)
}

func clearReportArtifacts(reportRoot string) error {
	paths := []string{
		filepath.Join(reportRoot, "lanes"),
		filepath.Join(reportRoot, results.SummaryFileName),
		filepath.Join(reportRoot, results.MarkdownFileName),
		filepath.Join(reportRoot, runmanifest.FileName),
	}
	for _, path := range paths {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("clear report artifact %s: %w", path, err)
		}
	}
	return nil
}

func (runner *Runner) collectManifest(ctx context.Context, plan runPlan) runmanifest.Manifest {
	configuration := runner.configuration
	record := runmanifest.Manifest{
		Backend: configuration.Backend,
		Lanes:   make([]runmanifest.Lane, len(plan.lanes)),
	}
	for index, lane := range plan.lanes {
		suites := make([]string, len(lane.definition.Suites))
		for position, id := range lane.definition.Suites {
			suites[position] = string(id)
		}
		record.Lanes[index] = runmanifest.Lane{
			Name:    lane.definition.Name,
			Enclave: lane.enclaveName,
			Profile: lane.definition.Profile,
			Suites:  suites,
			Seed:    lane.seed,
		}
	}

	// Attached networks run whatever they were provisioned with; recording
	// this run's image configuration there would only mislead.
	if plan.mode.provisionsNetwork() {
		record.PackageLocator = devnet.PackageLocator
		if len(configuration.Parameters) != 0 {
			record.ParametersSHA256 = fmt.Sprintf("%x", sha256.Sum256(configuration.Parameters))
		} else if images, err := configuration.Images.Resolved(); err == nil {
			record.Images = &images
		} else {
			raw := configuration.Images
			record.Images = &raw
		}
	}

	return runmanifest.Enrich(ctx, plan.testsDir, record)
}

func (runner *Runner) runLanes(ctx context.Context, plan runPlan) []results.Outcome {
	limit := runner.configuration.MaxParallel
	outcomes := make([]results.Outcome, len(plan.lanes))

	if limit < 2 || len(plan.lanes) < 2 {
		for index, lane := range plan.lanes {
			outcomes[index] = runner.runLane(ctx, plan, lane)
		}
		return outcomes
	}

	semaphore := make(chan struct{}, limit)
	var group sync.WaitGroup
	for index, lane := range plan.lanes {
		group.Go(func() {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			outcomes[index] = runner.runLane(ctx, plan, lane)
		})
	}
	group.Wait()

	return outcomes
}
