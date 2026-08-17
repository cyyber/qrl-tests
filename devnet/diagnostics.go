package devnet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cyyber/qrl-tests/internal/jsonfile"
)

type diagnosticsCommand func(ctx context.Context, output io.Writer, name string, arguments ...string) error

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
	manifestErr := jsonfile.Write(filepath.Join(outputDir, "diagnostics.json"), manifest, "diagnostics manifest")

	return errors.Join(inspectionErr, servicesErr, manifestErr)
}

func collectInspection(
	ctx context.Context,
	run diagnosticsCommand,
	enclaveName,
	outputDir string,
) (inspectionDiagnostic, string, error) {
	inspection := inspectionDiagnostic{File: "inspect.txt"}

	var buffer strings.Builder
	commandErr := run(ctx, &buffer, "kurtosis", "enclave", "inspect", enclaveName)
	if commandErr != nil {
		commandErr = fmt.Errorf("kurtosis enclave inspect %s: %w", enclaveName, commandErr)
	}
	output := buffer.String()
	writeErr := writeDiagnostic(filepath.Join(outputDir, inspection.File), output)
	captureErr := errors.Join(commandErr, writeErr)

	inspection.Captured = captureErr == nil
	if captureErr != nil {
		inspection.Error = captureErr.Error()
	}

	return inspection, output, captureErr
}

func collectServiceLogs(
	ctx context.Context,
	run diagnosticsCommand,
	enclaveName,
	outputDir,
	inspectionOutput string,
) ([]serviceDiagnostic, error) {
	serviceNames := serviceNamesFromInspection(inspectionOutput)
	if len(serviceNames) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "services"), 0o755); err != nil {
		return nil, fmt.Errorf("create service diagnostics directory: %w", err)
	}

	serviceDiagnostics := make([]serviceDiagnostic, 0, len(serviceNames))
	var collectionErrors []error
	for _, service := range serviceNames {
		diagnostic, err := collectServiceLog(ctx, run, enclaveName, outputDir, service)
		serviceDiagnostics = append(serviceDiagnostics, diagnostic)
		if err != nil {
			collectionErrors = append(collectionErrors, err)
		}
	}
	return serviceDiagnostics, errors.Join(collectionErrors...)
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
	path := filepath.Join(outputDir, relativePath)

	output, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		err = fmt.Errorf("write diagnostic %s: %w", path, err)
		diagnostic.Error = err.Error()
		return diagnostic, err
	}
	commandErr := run(ctx, output, "kurtosis", "service", "logs", "--all", enclaveName, service)
	if commandErr != nil {
		commandErr = fmt.Errorf("kurtosis service logs %s %s: %w", enclaveName, service, commandErr)
	}
	closeErr := output.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("write diagnostic %s: %w", path, closeErr)
	}
	captureErr := errors.Join(commandErr, closeErr)
	diagnostic.Captured = captureErr == nil
	if captureErr != nil {
		diagnostic.Error = captureErr.Error()
	}

	return diagnostic, captureErr
}

func serviceNamesFromInspection(inspection string) []string {
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
