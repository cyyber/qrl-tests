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

// The CLI is already a hard requirement of the network lifecycle and exposes
// inspection and log commands that are not available as one SDK operation.
type commandRunner func(ctx context.Context, name string, arguments ...string) (string, error)

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

// Collect captures the enclave inspection and per-service logs before cleanup.
// Every step is best-effort; the joined error reports what could not be saved.
func (manager *Manager) Collect(ctx context.Context, enclave, outputDir string) error {
	return manager.collect(ctx, enclave, outputDir)
}

func collectDiagnostics(ctx context.Context, run commandRunner, enclave, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create diagnostics directory: %w", err)
	}

	report := diagnosticsManifest{Enclave: enclave}
	var problems []error

	inspection, inspectErr := run(ctx, "kurtosis", "enclave", "inspect", enclave)
	report.Inspection = diagnosticCapture{File: "inspect.txt", Captured: inspectErr == nil}
	if inspectErr != nil {
		report.Inspection.Error = inspectErr.Error()
		problems = append(problems, fmt.Errorf("kurtosis enclave inspect %s: %w", enclave, inspectErr))
	}
	if err := writeDiagnostic(filepath.Join(outputDir, report.Inspection.File), inspection); err != nil {
		report.Inspection.Captured = false
		report.Inspection.Error = err.Error()
		problems = append(problems, err)
	}

	services := diagnosticServices(inspection)
	if len(services) > 0 {
		if err := os.MkdirAll(filepath.Join(outputDir, "services"), 0o755); err != nil {
			problems = append(problems, fmt.Errorf("create service diagnostics directory: %w", err))
		} else {
			for _, service := range services {
				record, err := collectServiceLog(ctx, run, enclave, outputDir, service)
				report.Services = append(report.Services, record)
				if err != nil {
					problems = append(problems, err)
				}
			}
		}
	}

	if err := writeDiagnosticsManifest(outputDir, report); err != nil {
		problems = append(problems, err)
	}
	return errors.Join(problems...)
}

func collectServiceLog(
	ctx context.Context,
	run commandRunner,
	enclave,
	outputDir,
	service string,
) (serviceDiagnostic, error) {
	relativePath := filepath.Join("services", service+".log")
	record := serviceDiagnostic{
		Name:      service,
		File:      filepath.ToSlash(relativePath),
		Captured:  true,
		Sanitized: !isRuntimeService(service),
	}

	output, commandErr := run(ctx, "kurtosis", "service", "logs", "--all", enclave, service)
	if record.Sanitized {
		output = sanitizeProvisioningLog(output)
	}
	writeErr := writeDiagnostic(filepath.Join(outputDir, relativePath), output)

	var problems []error
	if commandErr != nil {
		record.Captured = false
		record.Error = commandErr.Error()
		problems = append(problems, fmt.Errorf("kurtosis service logs %s %s: %w", enclave, service, commandErr))
	}
	if writeErr != nil {
		record.Captured = false
		record.Error = writeErr.Error()
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

func writeDiagnosticsManifest(outputDir string, report diagnosticsManifest) error {
	payload, err := json.MarshalIndent(report, "", "  ")
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
