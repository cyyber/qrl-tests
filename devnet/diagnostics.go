package devnet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type diagnosticsCommand func(ctx context.Context, name string, arguments ...string) (string, error)

type diagnosticCapture struct {
	File     string `json:"file"`
	Captured bool   `json:"captured"`
	Error    string `json:"error,omitempty"`
}

type serviceDiagnostic struct {
	Name      string `json:"name"`
	File      string `json:"file"`
	Captured  bool   `json:"captured"`
	Sanitized bool   `json:"sanitized,omitempty"`
	Error     string `json:"error,omitempty"`
}

type diagnosticsManifest struct {
	Enclave    string              `json:"enclave"`
	Inspection diagnosticCapture   `json:"inspection"`
	Services   []serviceDiagnostic `json:"services"`
}

// CollectDiagnostics captures the enclave inspection and per-service logs.
// It continues after individual capture failures and returns their joined error.
func (manager *Manager) CollectDiagnostics(ctx context.Context, enclaveName, outputDir string) error {
	return manager.collectDiagnostics(ctx, enclaveName, outputDir)
}

func collectDiagnostics(ctx context.Context, run diagnosticsCommand, enclaveName, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create diagnostics directory: %w", err)
	}

	manifest := diagnosticsManifest{Enclave: enclaveName}
	var problems []error

	inspection, commandErr := run(ctx, "kurtosis", "enclave", "inspect", enclaveName)
	manifest.Inspection = diagnosticCapture{File: "inspect.txt"}
	writeErr := writeDiagnostic(filepath.Join(outputDir, manifest.Inspection.File), inspection)
	captureErr := errors.Join(commandErr, writeErr)
	manifest.Inspection.Captured = captureErr == nil
	if captureErr != nil {
		manifest.Inspection.Error = captureErr.Error()
	}
	if commandErr != nil {
		problems = append(problems, fmt.Errorf("kurtosis enclave inspect %s: %w", enclaveName, commandErr))
	}
	if writeErr != nil {
		problems = append(problems, writeErr)
	}

	services := diagnosticServices(inspection)
	if len(services) > 0 {
		if err := os.MkdirAll(filepath.Join(outputDir, "services"), 0o755); err != nil {
			problems = append(problems, fmt.Errorf("create service diagnostics directory: %w", err))
		} else {
			for _, service := range services {
				record, err := collectServiceLog(ctx, run, enclaveName, outputDir, service)
				manifest.Services = append(manifest.Services, record)
				if err != nil {
					problems = append(problems, err)
				}
			}
		}
	}

	if err := writeDiagnosticsManifest(outputDir, manifest); err != nil {
		problems = append(problems, err)
	}
	return errors.Join(problems...)
}

func collectServiceLog(
	ctx context.Context,
	run diagnosticsCommand,
	enclaveName,
	outputDir,
	service string,
) (serviceDiagnostic, error) {
	relativePath := filepath.Join("services", service+".log")
	record := serviceDiagnostic{
		Name:      service,
		File:      filepath.ToSlash(relativePath),
		Sanitized: !isRuntimeService(service),
	}

	output, commandErr := run(ctx, "kurtosis", "service", "logs", "--all", enclaveName, service)
	if record.Sanitized {
		output = sanitizeProvisioningLog(output)
	}
	writeErr := writeDiagnostic(filepath.Join(outputDir, relativePath), output)
	captureErr := errors.Join(commandErr, writeErr)
	record.Captured = captureErr == nil
	if captureErr != nil {
		record.Error = captureErr.Error()
	}

	var problems []error
	if commandErr != nil {
		problems = append(problems, fmt.Errorf("kurtosis service logs %s %s: %w", enclaveName, service, commandErr))
	}
	if writeErr != nil {
		problems = append(problems, writeErr)
	}
	return record, errors.Join(problems...)
}

func diagnosticServices(inspection string) []string {
	var services []string
	inServices := false
	for line := range strings.Lines(inspection) {
		if strings.Contains(line, "User Services") {
			inServices = true
			continue
		}
		if !inServices {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "===") {
			break
		}

		fields := strings.Fields(line)
		if len(fields) < 2 || !isHex(fields[0]) {
			continue
		}
		services = append(services, fields[1])
	}
	return services
}

func isHex(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return false
		}
	}
	return true
}

func isRuntimeService(name string) bool {
	return strings.HasPrefix(name, "el-") ||
		strings.HasPrefix(name, "cl-") ||
		strings.HasPrefix(name, "vc-") ||
		strings.HasPrefix(name, "signer-")
}

func sanitizeProvisioningLog(output string) string {
	var sanitized strings.Builder
	redacted := false
	for line := range strings.Lines(output) {
		if sensitiveDiagnosticLine(line) {
			if !redacted {
				sanitized.WriteString("[redacted sensitive diagnostic output]\n")
				redacted = true
			}
			continue
		}
		redacted = false
		sanitized.WriteString(line)
	}
	return sanitized.String()
}

func sensitiveDiagnosticLine(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range []string{
		"seed", "password", "jwt", "secret",
		"private key", "private-key", "private_key",
		"\"ciphertext\"", "\"crypto\"",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func writeDiagnostic(path, output string) error {
	if err := os.WriteFile(path, []byte(output), 0o600); err != nil {
		return fmt.Errorf("write diagnostic %s: %w", path, err)
	}
	return nil
}

func writeDiagnosticsManifest(outputDir string, manifest diagnosticsManifest) error {
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode diagnostics manifest: %w", err)
	}
	return writeDiagnostic(filepath.Join(outputDir, "diagnostics.json"), string(append(payload, '\n')))
}

func runDiagnosticsCommand(ctx context.Context, name string, arguments ...string) (string, error) {
	// Combined output: failures usually explain themselves on stderr, and the
	// captured file is more useful with that explanation in it.
	output, err := exec.CommandContext(ctx, name, arguments...).CombinedOutput()
	return string(output), err
}
