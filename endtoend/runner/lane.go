// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

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
	"github.com/cyyber/qrl-tests/endtoend/internal/runenv"
)

const laneCleanupTimeout = 2 * time.Minute

type laneLease struct {
	environment devnet.Environment
	release     func() error
}

func (runner *Runner) acquireLane(ctx context.Context, planned laneRun) (laneLease, error) {
	if !planned.provision {
		environment, err := runner.networks.Inspect(ctx, planned.enclaveName, runner.configuration.Backend)
		if err != nil {
			return laneLease{}, fmt.Errorf("lane %s: inspect network: %w", planned.lane.Name, err)
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
		return laneLease{}, fmt.Errorf("lane %s: start network: %w", planned.lane.Name, err)
	}
	return laneLease{
		environment: environment,
		release: func() error {
			stopCtx, cancel := context.WithTimeout(context.Background(), laneCleanupTimeout)
			defer cancel()
			if err := runner.networks.Stop(stopCtx, planned.enclaveName); err != nil {
				return fmt.Errorf("lane %s: stop network: %w", planned.lane.Name, err)
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

func (runner *Runner) runLane(ctx context.Context, planned laneRun) (result error) {
	lane := planned.lane
	if err := os.MkdirAll(planned.reportDir, 0o755); err != nil {
		return fmt.Errorf("lane %s: create report directory: %w", lane.Name, err)
	}
	logFile, err := os.OpenFile(filepath.Join(planned.reportDir, "output.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("lane %s: create output log: %w", lane.Name, err)
	}
	defer func() { result = errors.Join(result, logFile.Close()) }()
	laneLog := &lockedWriter{lock: new(sync.Mutex), writer: logFile}
	stdout := io.MultiWriter(runner.stdout, laneLog)
	stderr := io.MultiWriter(runner.stderr, laneLog)

	lease, err := runner.acquireLane(ctx, planned)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, lease.close()) }()

	if err := runenv.Write(planned.manifestPath, runenv.Manifest{
		Lane:        lane.Name,
		Profile:     lane.Profile,
		Environment: lease.environment,
	}); err != nil {
		return fmt.Errorf("lane %s: %w", lane.Name, err)
	}

	laneCtx, cancelLane := context.WithTimeout(ctx, lane.Timeout+5*time.Minute)
	defer cancelLane()
	fmt.Fprintf(stdout, "=== RUN lane=%s profile=%s ===\n", lane.Name, lane.Profile)
	environment := append(os.Environ(), runenv.PathEnv+"="+planned.manifestPath)
	if planned.workspace != "" {
		environment = setEnvironment(environment, "GOWORK", planned.workspace)
	}
	if err := runner.runCommand(laneCtx, commandSpec{
		Path:   "go",
		Args:   planned.arguments,
		Dir:    planned.testsDir,
		Env:    environment,
		Stdout: stdout,
		Stderr: stderr,
	}); err != nil {
		return fmt.Errorf("lane %s: %w", lane.Name, err)
	}
	return nil
}
