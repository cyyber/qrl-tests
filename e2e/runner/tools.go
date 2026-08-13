package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/manifest"
)

type outputCommand func(context.Context, string, ...string) ([]byte, error)

const toolCleanupTimeout = 30 * time.Second

func executeOutput(ctx context.Context, name string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	output, err := command.Output()
	if err == nil {
		return output, nil
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		if detail := strings.TrimSpace(string(exitError.Stderr)); detail != "" {
			return output, fmt.Errorf("%w: %s", err, detail)
		}
	}
	return output, err
}

func (runner *Runner) prepareLaneTools(
	ctx context.Context,
	plan runPlan,
	planned laneRun,
) (manifest.Tools, func() error, error) {
	if !planned.lane.NeedsGQRL() {
		return manifest.Tools{}, nil, nil
	}

	image := ""
	if plan.mode.provisions() && len(runner.configuration.Parameters) == 0 {
		images, err := runner.configuration.Images.Resolved()
		if err != nil {
			return manifest.Tools{}, nil, err
		}
		image = images.Execution
	}
	directory, err := os.MkdirTemp("", "qrl-tests-"+planned.lane.Name+"-")
	if err != nil {
		return manifest.Tools{}, nil, fmt.Errorf("create tool directory: %w", err)
	}
	cleanup := func() error {
		if err := runner.removeToolDir(directory); err != nil {
			return fmt.Errorf("remove tool directory: %w", err)
		}
		return nil
	}
	gqrl := filepath.Join(directory, "gqrl")
	if err := runner.prepareGQRL(ctx, plan.mode, runner.configuration.Backend, plan.testsDir, image, gqrl); err != nil {
		return manifest.Tools{}, nil, errors.Join(fmt.Errorf("prepare gqrl: %w", err), cleanup())
	}
	return manifest.Tools{GQRL: gqrl}, cleanup, nil
}

func prepareGQRL(
	ctx context.Context,
	goos string,
	mode runMode,
	backend devnet.Backend,
	testsDir string,
	image string,
	destination string,
	run outputCommand,
) error {
	if mode.provisions() && goos == "linux" && backend == devnet.BackendDocker && image != "" {
		return extractGQRL(ctx, image, destination, run)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create tool directory: %w", err)
	}
	if _, err := run(
		ctx,
		"go",
		"-C", testsDir,
		"build",
		"-o", destination,
		"github.com/theQRL/go-qrl/cmd/gqrl",
	); err != nil {
		return fmt.Errorf("build gqrl: %w", err)
	}
	return nil
}

func extractGQRL(
	ctx context.Context,
	image string,
	destination string,
	run outputCommand,
) (result error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create tool directory: %w", err)
	}

	output, err := run(ctx, "docker", "create", "--pull=missing", image)
	if err != nil {
		return fmt.Errorf("create execution image container: %w", err)
	}
	containerID := strings.TrimSpace(string(output))
	if containerID == "" {
		return errors.New("create execution image container: docker returned no container ID")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), toolCleanupTimeout)
		defer cancel()
		if _, err := run(cleanupCtx, "docker", "rm", "-f", containerID); err != nil {
			result = errors.Join(result, fmt.Errorf("remove execution image container: %w", err))
		}
	}()

	if _, err := run(ctx, "docker", "cp", containerID+":/usr/local/bin/gqrl", destination); err != nil {
		return fmt.Errorf("copy /usr/local/bin/gqrl: %w", err)
	}
	if err := os.Chmod(destination, 0o755); err != nil {
		return fmt.Errorf("make gqrl executable: %w", err)
	}
	return nil
}
