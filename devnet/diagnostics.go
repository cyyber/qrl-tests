package devnet

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The CLI is already a hard requirement of the network lifecycle and exposes
// inspection and log commands that are not available as one SDK operation.
type commandRunner func(ctx context.Context, name string, arguments ...string) (string, error)

// Collect captures the enclave inspection and service logs before cleanup.
// Every step is best-effort; the joined error reports what could not be saved.
func (manager *Manager) Collect(ctx context.Context, enclave, outputDir string) error {
	return manager.collect(ctx, enclave, outputDir)
}

func collectDiagnostics(ctx context.Context, run commandRunner, enclave, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create diagnostics directory: %w", err)
	}

	var problems []error
	capture := func(file string, name string, arguments ...string) {
		output, err := run(ctx, name, arguments...)
		if err != nil {
			problems = append(problems, fmt.Errorf("%s %s: %w", name, strings.Join(arguments, " "), err))
		}
		if output == "" {
			return
		}
		if err := os.WriteFile(file, []byte(output), 0o600); err != nil {
			problems = append(problems, err)
		}
	}

	capture(filepath.Join(outputDir, "inspect.txt"), "kurtosis", "enclave", "inspect", enclave)
	capture(filepath.Join(outputDir, "services.log"),
		"kurtosis", "service", "logs", "--all-services", "--all", enclave)

	return errors.Join(problems...)
}

func runDiagnosticsCommand(ctx context.Context, name string, arguments ...string) (string, error) {
	// Combined output: failures usually explain themselves on stderr, and the
	// captured file is more useful with that explanation in it.
	output, err := exec.CommandContext(ctx, name, arguments...).CombinedOutput()
	return string(output), err
}
