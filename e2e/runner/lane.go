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

func (runner *Runner) acquireLane(ctx context.Context, planned laneRun) (laneLease, error) {
	if !planned.provision {
		environment, err := runner.networks.Inspect(ctx, planned.enclaveName)
		if err != nil {
			return laneLease{}, fmt.Errorf("inspect network: %w", err)
		}
		return laneLease{environment: environment}, nil
	}

	startCtx, cancelStart := context.WithTimeout(ctx, runner.configuration.StartTimeout)
	environment, err := runner.networks.Start(startCtx, devnet.StartOptions{
		EnclaveName: planned.enclaveName,
		Backend:     runner.configuration.Backend,
		Images:      runner.configuration.Images,
		Parameters:  runner.configuration.Parameters,
		Profile:     planned.lane.Profile,
	})
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
	lease, err := runner.acquireLane(ctx, planned)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, lease.close()) }()

	if err := os.MkdirAll(planned.reportDir, 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	logFile, err := os.OpenFile(filepath.Join(planned.reportDir, "output.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create output log: %w", err)
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
		return err
	}

	laneCtx, cancelLane := context.WithTimeout(ctx, lane.Timeout+laneReportSlack)
	defer cancelLane()
	fmt.Fprintf(stdout, "=== RUN lane=%s profile=%s ===\n", lane.Name, lane.Profile)
	processEnvironment := append(os.Environ(), manifest.PathEnv+"="+planned.manifestPath)
	return runner.runCommand(laneCtx, commandSpec{
		Path:   "go",
		Args:   planned.arguments,
		Dir:    planned.testsDir,
		Env:    processEnvironment,
		Stdout: stdout,
		Stderr: stderr,
	})
}
