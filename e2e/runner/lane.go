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
	laneCleanupTimeout = 2 * time.Minute

	// laneReportSlack extends the lane context past ginkgo's own --timeout so
	// it can report and clean up before the context interrupts the process.
	laneReportSlack = 5 * time.Minute
)

type laneLease struct {
	environment devnet.Environment
	release     func() error
}

func (runner *Runner) acquireLane(ctx context.Context, plan runPlan, lane laneRun) (laneLease, error) {
	if !plan.mode.provisionsNetwork() {
		environment, err := runner.networks.Inspect(ctx, lane.enclaveName)
		if err != nil {
			return laneLease{}, fmt.Errorf("inspect network: %w", err)
		}
		return laneLease{environment: environment}, nil
	}

	options := devnet.StartOptions{
		EnclaveName: lane.enclaveName,
		Backend:     runner.configuration.Backend,
		Images:      runner.configuration.Images,
		Parameters:  runner.configuration.Parameters,
		Profile:     lane.definition.Profile,
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

func (runner *Runner) runLane(ctx context.Context, plan runPlan, lane laneRun) results.Outcome {
	outcome := runner.executeLane(ctx, plan, lane)
	if outcome.Err != nil {
		outcome.Err = fmt.Errorf("lane %s: %w", lane.definition.Name, outcome.Err)
	}
	return outcome
}

func (runner *Runner) executeLane(ctx context.Context, plan runPlan, lane laneRun) (outcome results.Outcome) {
	definition := lane.definition
	outcome.Name = definition.Name
	lease, err := runner.acquireLane(ctx, plan, lane)
	if err != nil {
		outcome.BootstrapFailure = true
		outcome.Err = fmt.Errorf("network bootstrap failed: %w", err)
		return outcome
	}
	var logFile *os.File
	defer func() {
		outcome.Err = errors.Join(outcome.Err, lease.close())
		if logFile != nil {
			outcome.Err = errors.Join(outcome.Err, logFile.Close())
		}
	}()

	if err := os.MkdirAll(lane.reportDir, 0o755); err != nil {
		outcome.Err = fmt.Errorf("test infrastructure failed: create report directory: %w", err)
		return outcome
	}
	logFile, err = os.OpenFile(filepath.Join(lane.reportDir, "output.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		outcome.Err = fmt.Errorf("test infrastructure failed: create output log: %w", err)
		return outcome
	}
	laneLog := &lockedWriter{lock: new(sync.Mutex), writer: logFile}
	stdout := io.MultiWriter(runner.stdout, laneLog)
	stderr := io.MultiWriter(runner.stderr, laneLog)
	manifestPath := lane.manifestPath()
	if err := manifest.Write(manifestPath, manifest.Manifest{
		Lane:        definition.Name,
		Profile:     definition.Profile,
		Environment: lease.environment,
	}); err != nil {
		outcome.Err = fmt.Errorf("test infrastructure failed: %w", err)
		return outcome
	}

	laneCtx, cancelLane := context.WithTimeout(ctx, definition.Timeout+laneReportSlack)
	defer cancelLane()
	fmt.Fprintf(stdout, "=== RUN lane=%s profile=%s ===\n", definition.Name, definition.Profile)
	processEnvironment := append(os.Environ(), manifest.PathEnv+"="+manifestPath)
	outcome.ExecutionErr = runner.runCommand(laneCtx, commandSpec{
		Path:   "go",
		Args:   lane.ginkgoArguments(),
		Dir:    plan.testsDir,
		Env:    processEnvironment,
		Stdout: stdout,
		Stderr: stderr,
	})
	outcome.ExecutionErr = errors.Join(outcome.ExecutionErr, laneCtx.Err())
	outcome.Err = outcome.ExecutionErr
	outcome.Observation = results.Observe(lane.reportDir)
	return outcome
}
