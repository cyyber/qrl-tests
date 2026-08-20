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

const (
	toolCleanupTimeout = 30 * time.Second

	gqrlModulePath  = "github.com/theQRL/go-qrl"
	gqrlCommandPath = gqrlModulePath + "/cmd/gqrl"

	gqrlBuildModuleName = "gqrlbuild"

	// gqrlPinFormat prints the go-qrl version the tests module requires,
	// followed by the module replacing it when the tests module replaces it.
	gqrlPinFormat = "{{.Version}} {{with .Replace}}{{.Path}}@{{.Version}}{{end}}"
)

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
	lane laneRun,
) (manifest.Tools, func() error, error) {
	if !lane.definition.NeedsGQRL() {
		return manifest.Tools{}, nil, nil
	}

	image := ""
	if plan.mode.provisionsNetwork() && len(runner.configuration.Parameters) == 0 {
		images, err := runner.configuration.Images.Resolved()
		if err != nil {
			return manifest.Tools{}, nil, err
		}
		image = images.Execution
	}
	directory, err := os.MkdirTemp("", "qrl-tests-"+lane.definition.Name+"-")
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
	if mode.provisionsNetwork() && goos == "linux" && backend == devnet.BackendDocker && image != "" {
		return extractGQRL(ctx, image, destination, run)
	}

	return buildGQRL(ctx, testsDir, destination, run)
}

// buildGQRL compiles the pinned gqrl command in a throwaway module so the tool
// is built against go-qrl's own dependency graph. Building it inside the tests
// module cannot work: minimum version selection there also weighs the test
// tooling's requirements, which upgrade shared modules past the releases go-qrl
// compiles against, and the tests module records no sums for the dependencies
// only gqrl pulls in.
func buildGQRL(ctx context.Context, testsDir, destination string, run outputCommand) (result error) {
	pin, err := readGQRLPin(ctx, testsDir, run)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create tool directory: %w", err)
	}

	module, err := os.MkdirTemp("", "qrl-tests-gqrl-build-")
	if err != nil {
		return fmt.Errorf("create gqrl build module: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(module); err != nil {
			result = errors.Join(result, fmt.Errorf("remove gqrl build module: %w", err))
		}
	}()

	if _, err := run(ctx, "go", "-C", module, "mod", "init", gqrlBuildModuleName); err != nil {
		return fmt.Errorf("initialize gqrl build module: %w", err)
	}

	if pin.replacement != "" {
		if _, err := run(ctx, "go", "-C", module, "mod", "edit", "-replace", gqrlModulePath+"="+pin.replacement); err != nil {
			return fmt.Errorf("replace go-qrl in gqrl build module: %w", err)
		}
	}

	// The requirement is resolved before building so that go records the sums
	// of the whole go-qrl graph in the throwaway module.
	if _, err := run(ctx, "go", "-C", module, "get", gqrlCommandPath+"@"+pin.version); err != nil {
		return fmt.Errorf("resolve gqrl dependencies: %w", err)
	}

	if _, err := run(ctx, "go", "-C", module, "build", "-o", destination, gqrlCommandPath); err != nil {
		return fmt.Errorf("build gqrl: %w", err)
	}

	return nil
}

type gqrlPin struct {
	version     string
	replacement string
}

// readGQRLPin reports how the tests module pins go-qrl, which the build module
// repeats so both resolve the same source.
func readGQRLPin(ctx context.Context, testsDir string, run outputCommand) (gqrlPin, error) {
	output, err := run(ctx, "go", "-C", testsDir, "list", "-m", "-f", gqrlPinFormat, gqrlModulePath)
	if err != nil {
		return gqrlPin{}, fmt.Errorf("read pinned go-qrl module: %w", err)
	}

	fields := strings.Fields(string(output))
	if len(fields) == 0 {
		return gqrlPin{}, fmt.Errorf("read pinned go-qrl module: %s reports no version", gqrlModulePath)
	}

	pin := gqrlPin{version: fields[0]}
	if len(fields) > 1 {
		pin.replacement = fields[1]
	}

	return pin, nil
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
