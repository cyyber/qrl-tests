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

// The CLI is already a hard requirement of the network lifecycle, and its
// enclave dump captures what the SDK offers no single call for: every service
// log plus the container inspections.
type commandRunner func(ctx context.Context, name string, arguments ...string) (string, error)

// Collect gathers the enclave's diagnostics into outputDir before the enclave
// is destroyed: the Kurtosis inspection and dump under kurtosis/, and — on the
// Docker backend — container state and resource usage under runtime/. Every
// step is best-effort; the joined error reports what could not be captured.
func (manager *Manager) Collect(ctx context.Context, backend Backend, enclave, outputDir string) error {
	return manager.collect(ctx, backend, enclave, outputDir)
}

func collectDiagnostics(ctx context.Context, run commandRunner, backend Backend, enclave, outputDir string) error {
	kurtosisDir := filepath.Join(outputDir, "kurtosis")
	if err := os.MkdirAll(kurtosisDir, 0o755); err != nil {
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

	capture(filepath.Join(kurtosisDir, "inspect.txt"), "kurtosis", "enclave", "inspect", enclave)
	// The dump writes its own tree: per-service logs and container inspections.
	if _, err := run(ctx, "kurtosis", "enclave", "dump", enclave, filepath.Join(kurtosisDir, "dump")); err != nil {
		problems = append(problems, fmt.Errorf("kurtosis enclave dump: %w", err))
	}

	if backend == BackendDocker {
		runtimeDir := filepath.Join(outputDir, "runtime")
		if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
			problems = append(problems, err)
		} else {
			capture(filepath.Join(runtimeDir, "containers.txt"), "docker", "ps", "--all", "--no-trunc")
			capture(filepath.Join(runtimeDir, "stats.txt"), "docker", "stats", "--all", "--no-stream")
		}
	}

	return errors.Join(problems...)
}

func runDiagnosticsCommand(ctx context.Context, name string, arguments ...string) (string, error) {
	// Combined output: failures usually explain themselves on stderr, and the
	// captured file is more useful with that explanation in it.
	output, err := exec.CommandContext(ctx, name, arguments...).CombinedOutput()
	return string(output), err
}
