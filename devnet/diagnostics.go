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

// Collect captures the enclave inspection and client logs before cleanup.
// Every step is best-effort; the joined error reports what could not be saved.
func (manager *Manager) Collect(ctx context.Context, enclave, outputDir string) error {
	return manager.collect(ctx, enclave, outputDir)
}

func collectDiagnostics(ctx context.Context, run commandRunner, enclave, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create diagnostics directory: %w", err)
	}

	var problems []error
	capture := func(file string, name string, arguments ...string) string {
		output, err := run(ctx, name, arguments...)
		if err != nil {
			problems = append(problems, fmt.Errorf("%s %s: %w", name, strings.Join(arguments, " "), err))
		}
		if output == "" {
			return output
		}
		if err := os.WriteFile(file, []byte(output), 0o600); err != nil {
			problems = append(problems, err)
		}
		return output
	}

	inspection := capture(filepath.Join(outputDir, "inspect.txt"),
		"kurtosis", "enclave", "inspect", enclave)
	services := diagnosticServices(inspection)
	if len(services) > 0 {
		arguments := []string{"service", "logs", "--num", "200", enclave}
		capture(filepath.Join(outputDir, "services.log"),
			"kurtosis", append(arguments, services...)...)
	}

	return errors.Join(problems...)
}

func diagnosticServices(inspection string) []string {
	var services []string
	for line := range strings.Lines(inspection) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[1]
		if strings.HasPrefix(name, "el-") ||
			strings.HasPrefix(name, "cl-") ||
			strings.HasPrefix(name, "vc-") ||
			strings.HasPrefix(name, "signer-") {
			services = append(services, name)
		}
	}
	return services
}

func runDiagnosticsCommand(ctx context.Context, name string, arguments ...string) (string, error) {
	// Combined output: failures usually explain themselves on stderr, and the
	// captured file is more useful with that explanation in it.
	output, err := exec.CommandContext(ctx, name, arguments...).CombinedOutput()
	return string(output), err
}
