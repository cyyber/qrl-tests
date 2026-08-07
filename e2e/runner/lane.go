package runner

import (
	"cmp"
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

func (runner *Runner) acquireLane(ctx context.Context, planned laneRun) (laneLease, error) {
	if !planned.provision {
		environment, err := runner.networks.Inspect(ctx, planned.enclaveName)
		if err != nil {
			return laneLease{}, fmt.Errorf("inspect network: %w", err)
		}
		return laneLease{environment: environment}, nil
	}

	options := devnet.StartOptions{
		EnclaveName:    planned.enclaveName,
		Backend:        runner.configuration.Backend,
		Images:         runner.configuration.Images,
		Parameters:     runner.configuration.Parameters,
		Profile:        planned.lane.Profile,
		PackageLocator: runner.configuration.PackageLocator,
	}
	if runner.diagnosticsMode() != DiagnosticsNever {
		options.FailureDiagnosticsDir = planned.diagnosticsDir
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
			if err := runner.networks.Stop(stopCtx, planned.enclaveName); err != nil {
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

func (runner *Runner) runLane(ctx context.Context, planned laneRun) error {
	if err := runner.executeLane(ctx, planned); err != nil {
		return fmt.Errorf("lane %s: %w", planned.lane.Name, err)
	}
	return nil
}

func (runner *Runner) executeLane(ctx context.Context, planned laneRun) (result error) {
	lane := planned.lane
	// The sentinels feed failure classification: everything before the test
	// process starts is either network bootstrap or harness infrastructure.
	lease, err := runner.acquireLane(ctx, planned)
	if err != nil {
		return fmt.Errorf("%w: %w", results.ErrBootstrap, err)
	}
	defer func() { result = errors.Join(result, lease.close()) }()

	if err := os.MkdirAll(planned.reportDir, 0o755); err != nil {
		return fmt.Errorf("%w: create report directory: %w", results.ErrInfrastructure, err)
	}
	logFile, err := os.OpenFile(filepath.Join(planned.reportDir, "output.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("%w: create output log: %w", results.ErrInfrastructure, err)
	}
	defer func() { result = errors.Join(result, logFile.Close()) }()

	laneLog := &lockedWriter{lock: new(sync.Mutex), writer: logFile}
	stdout := io.MultiWriter(runner.stdout, laneLog)
	stderr := io.MultiWriter(runner.stderr, laneLog)

	if err := manifest.Write(planned.manifestPath, manifest.Manifest{
		Lane:        lane.Name,
		Profile:     lane.Profile,
		Environment: lease.environment,
	}); err != nil {
		return fmt.Errorf("%w: %w", results.ErrInfrastructure, err)
	}

	laneCtx, cancelLane := context.WithTimeout(ctx, lane.Timeout+laneReportSlack)
	defer cancelLane()
	fmt.Fprintf(stdout, "=== RUN lane=%s profile=%s ===\n", lane.Name, lane.Profile)
	processEnvironment := append(os.Environ(), manifest.PathEnv+"="+planned.manifestPath)
	runErr := runner.runCommand(laneCtx, commandSpec{
		Path:   "go",
		Args:   planned.arguments,
		Dir:    planned.testsDir,
		Env:    processEnvironment,
		Stdout: stdout,
		Stderr: stderr,
	})

	// Diagnostics happen here, before the deferred release destroys the
	// enclave. A collection problem never converts a passing lane into a
	// failing one; on a failing lane it is reported alongside the failure.
	if err := runner.collectDiagnostics(planned, lease.environment, runErr != nil); err != nil {
		if runErr != nil {
			return errors.Join(runErr, fmt.Errorf("collect diagnostics: %w", err))
		}
		fmt.Fprintf(stderr, "collect diagnostics: %v\n", err)
	}
	return runErr
}

func (runner *Runner) diagnosticsMode() DiagnosticsMode {
	if runner.configuration.Diagnostics == "" {
		return DiagnosticsOnFailure
	}
	return runner.configuration.Diagnostics
}

func (runner *Runner) collectDiagnostics(planned laneRun, environment devnet.Environment, failed bool) error {
	mode := runner.diagnosticsMode()
	if mode == DiagnosticsNever || (mode == DiagnosticsOnFailure && !failed) {
		return nil
	}

	// A fresh context: the lane context is already canceled when the lane
	// timed out, which is exactly when diagnostics matter most.
	collectCtx, cancel := context.WithTimeout(context.Background(), laneDiagnosticsTimeout)
	defer cancel()
	backend := cmp.Or(environment.Backend, runner.configuration.Backend)
	return runner.networks.Collect(collectCtx, backend, planned.enclaveName, planned.diagnosticsDir)
}
