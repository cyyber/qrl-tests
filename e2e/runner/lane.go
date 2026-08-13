package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/manifest"
	"github.com/cyyber/qrl-tests/internal/results"
)

const (
	laneCleanupTimeout     = 2 * time.Minute
	laneDiagnosticsTimeout = 5 * time.Minute

	// laneReportSlack extends the lane context past ginkgo's own --timeout so
	// it can report and clean up before the context interrupts the process.
	laneReportSlack = 5 * time.Minute
)

type laneLease struct {
	environment devnet.Environment
	release     func() error
}

func (runner *Runner) acquireLane(ctx context.Context, plan runPlan, planned laneRun) (laneLease, error) {
	if !plan.mode.provisions() {
		environment, err := runner.networks.Inspect(ctx, planned.enclaveName)
		if err != nil {
			return laneLease{}, fmt.Errorf("inspect network: %w", err)
		}
		return laneLease{environment: environment}, nil
	}

	options := devnet.StartOptions{
		EnclaveName:           planned.enclaveName,
		Backend:               runner.configuration.Backend,
		Images:                runner.configuration.Images,
		Parameters:            runner.configuration.Parameters,
		Profile:               planned.lane.Profile,
		FailureDiagnosticsDir: planned.diagnosticsDir,
	}

	startCtx, cancelStart := context.WithTimeout(ctx, runner.configuration.StartTimeout)
	environment, err := runner.networks.Start(startCtx, options)
	cancelStart()
	if err != nil {
		return laneLease{}, fmt.Errorf("start network: %w", err)
	}
	return laneLease{
		environment: environment,
		release: func() error {
			stopCtx, cancel := context.WithTimeout(context.Background(), laneCleanupTimeout)
			defer cancel()
			if err := runner.networks.Stop(stopCtx, environment.EnclaveName); err != nil {
				return fmt.Errorf("stop network: %w", err)
			}
			return nil
		},
	}, nil
}

func (lease laneLease) close() error {
	if lease.release == nil {
		return nil
	}
	return lease.release()
}

func (runner *Runner) runLane(ctx context.Context, plan runPlan, planned laneRun) results.Outcome {
	outcome := runner.executeLane(ctx, plan, planned)
	if outcome.Err != nil {
		outcome.Err = fmt.Errorf("lane %s: %w", planned.lane.Name, outcome.Err)
	}
	return outcome
}

func (runner *Runner) executeLane(ctx context.Context, plan runPlan, planned laneRun) (outcome results.Outcome) {
	outcome.Name = planned.lane.Name
	lane := planned.lane
	lease, err := runner.acquireLane(ctx, plan, planned)
	if err != nil {
		outcome.BootstrapFailure = true
		outcome.Err = fmt.Errorf("network bootstrap failed: %w", err)
		return outcome
	}
	var logFile *os.File
	defer func() {
		// Finalize while the enclave still exists. This covers command/report
		// failures as well as any harness failure after a successful acquire.
		failed := !outcome.Passed()
		if failed {
			if diagnosticsErr := runner.collectDiagnostics(planned, lease.environment); diagnosticsErr != nil {
				outcome.Err = errors.Join(outcome.Err, fmt.Errorf("collect diagnostics: %w", diagnosticsErr))
			}
		}
		outcome.Err = errors.Join(outcome.Err, lease.close())
		if logFile != nil {
			outcome.Err = errors.Join(outcome.Err, logFile.Close())
		}
	}()

	if err := os.MkdirAll(planned.reportDir, 0o755); err != nil {
		outcome.Err = fmt.Errorf("test infrastructure failed: create report directory: %w", err)
		return outcome
	}
	logFile, err = os.OpenFile(filepath.Join(planned.reportDir, "output.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		outcome.Err = fmt.Errorf("test infrastructure failed: create output log: %w", err)
		return outcome
	}
	laneLog := &lockedWriter{lock: new(sync.Mutex), writer: logFile}
	stdout := io.MultiWriter(runner.stdout, laneLog)
	stderr := io.MultiWriter(runner.stderr, laneLog)

	tools, cleanupTools, err := runner.prepareLaneTools(ctx, plan, planned)
	if err != nil {
		outcome.Err = fmt.Errorf("test infrastructure failed: %w", err)
		return outcome
	}
	if cleanupTools != nil {
		defer func() {
			outcome.Err = errors.Join(outcome.Err, cleanupTools())
		}()
	}

	manifestPath := planned.manifestPath()
	if err := manifest.Write(manifestPath, manifest.Manifest{
		Lane:        lane.Name,
		Profile:     lane.Profile,
		Environment: lease.environment,
		Tools:       tools,
	}); err != nil {
		outcome.Err = fmt.Errorf("test infrastructure failed: %w", err)
		return outcome
	}

	laneCtx, cancelLane := context.WithTimeout(ctx, lane.Timeout+laneReportSlack)
	defer cancelLane()
	fmt.Fprintf(stdout, "=== RUN lane=%s profile=%s ===\n", lane.Name, lane.Profile)
	processEnvironment := append(os.Environ(), manifest.PathEnv+"="+manifestPath)
	outcome.Err = runner.runCommand(laneCtx, commandSpec{
		Path:   "go",
		Args:   planned.ginkgoArguments(),
		Dir:    plan.testsDir,
		Env:    processEnvironment,
		Stdout: stdout,
		Stderr: stderr,
	})
	outcome.Observation = results.Observe(planned.reportDir)
	return outcome
}

func (runner *Runner) collectDiagnostics(planned laneRun, environment devnet.Environment) error {
	// A fresh context: the lane context is already canceled when the lane
	// timed out, which is exactly when diagnostics matter most.
	collectCtx, cancel := context.WithTimeout(context.Background(), laneDiagnosticsTimeout)
	defer cancel()
	return runner.networks.Collect(collectCtx, environment.EnclaveName, planned.diagnosticsDir)
}
