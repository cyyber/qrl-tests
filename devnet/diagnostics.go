package devnet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type diagnosticsCommand func(ctx context.Context, name string, arguments ...string) (string, error)

type inspectionDiagnostic struct {
	File     string `json:"file"`
	Captured bool   `json:"captured"`
	Error    string `json:"error,omitempty"`
}

type serviceDiagnostic struct {
	Name     string `json:"name"`
	File     string `json:"file"`
	Captured bool   `json:"captured"`
	Error    string `json:"error,omitempty"`
}

type diagnosticsManifest struct {
	Enclave    string               `json:"enclave"`
	Inspection inspectionDiagnostic `json:"inspection"`
	Services   []serviceDiagnostic  `json:"services"`
}

// CollectDiagnostics captures the enclave inspection and per-service logs.
// Collection continues after individual failures and returns all encountered errors.
func (manager *Manager) CollectDiagnostics(ctx context.Context, enclaveName, outputDir string) error {
	return manager.collectDiagnostics(ctx, enclaveName, outputDir)
}

func collectDiagnostics(ctx context.Context, run diagnosticsCommand, enclaveName, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create diagnostics directory: %w", err)
	}

	inspection, inspectionOutput, inspectionErr := collectInspection(ctx, run, enclaveName, outputDir)
	services, servicesErr := collectServiceLogs(ctx, run, enclaveName, outputDir, inspectionOutput)
	manifest := diagnosticsManifest{
		Enclave:    enclaveName,
		Inspection: inspection,
		Services:   services,
	}
	payload, manifestErr := json.MarshalIndent(manifest, "", "  ")
	if manifestErr != nil {
		manifestErr = fmt.Errorf("encode diagnostics manifest: %w", manifestErr)
	} else {
		manifestErr = writeDiagnostic(filepath.Join(outputDir, "diagnostics.json"), string(append(payload, '\n')))
	}

	return errors.Join(inspectionErr, servicesErr, manifestErr)
}

func collectInspection(
	ctx context.Context,
	run diagnosticsCommand,
	enclaveName,
	outputDir string,
) (inspectionDiagnostic, string, error) {
	inspection := inspectionDiagnostic{File: "inspect.txt"}

	output, commandErr := run(ctx, "kurtosis", "enclave", "inspect", enclaveName)
	writeErr := writeDiagnostic(filepath.Join(outputDir, inspection.File), output)
	captureErr := errors.Join(commandErr, writeErr)

	inspection.Captured = captureErr == nil
	if captureErr != nil {
		inspection.Error = captureErr.Error()
	}

	if commandErr != nil {
		commandErr = fmt.Errorf("kurtosis enclave inspect %s: %w", enclaveName, commandErr)
	}

	return inspection, output, errors.Join(commandErr, writeErr)
}

func collectServiceLogs(
	ctx context.Context,
	run diagnosticsCommand,
	enclaveName,
	outputDir,
	inspectionOutput string,
) ([]serviceDiagnostic, error) {
	services := diagnosticServices(inspectionOutput)
	if len(services) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "services"), 0o755); err != nil {
		return nil, fmt.Errorf("create service diagnostics directory: %w", err)
	}

	serviceDiagnostics := make([]serviceDiagnostic, 0, len(services))
	var problems []error
	for _, service := range services {
		diagnostic, err := collectServiceLog(ctx, run, enclaveName, outputDir, service)
		serviceDiagnostics = append(serviceDiagnostics, diagnostic)
		if err != nil {
			problems = append(problems, err)
		}
	}
	return serviceDiagnostics, errors.Join(problems...)
}

func collectServiceLog(
	ctx context.Context,
	run diagnosticsCommand,
	enclaveName,
	outputDir,
	service string,
) (serviceDiagnostic, error) {
	relativePath := filepath.Join("services", service+".log")
	diagnostic := serviceDiagnostic{Name: service, File: filepath.ToSlash(relativePath)}

	output, commandErr := run(ctx, "kurtosis", "service", "logs", "--all", enclaveName, service)
	writeErr := writeDiagnostic(filepath.Join(outputDir, relativePath), output)
	captureErr := errors.Join(commandErr, writeErr)
	diagnostic.Captured = captureErr == nil
	if captureErr != nil {
		diagnostic.Error = captureErr.Error()
	}

	if commandErr != nil {
		commandErr = fmt.Errorf("kurtosis service logs %s %s: %w", enclaveName, service, commandErr)
	}

	return diagnostic, errors.Join(commandErr, writeErr)
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

func writeDiagnostic(path, output string) error {
	if err := os.WriteFile(path, []byte(output), 0o600); err != nil {
		return fmt.Errorf("write diagnostic %s: %w", path, err)
	}
	return nil
}
