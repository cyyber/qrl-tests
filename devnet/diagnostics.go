package devnet

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cyyber/qrl-tests/devnet/internal/kurtosis"
	"github.com/cyyber/qrl-tests/internal/jsonfile"
)

// diagnosticsAPI is the direct engine API surface needed to capture failure
// artifacts. Connection ownership belongs to diagnosticsClient.
type diagnosticsAPI interface {
	Inspect(ctx context.Context, enclaveName string) (kurtosis.EnclaveInspection, error)
	ServiceLogs(
		ctx context.Context,
		enclaveName string,
		serviceUUIDs []string,
		consume kurtosis.ServiceLogConsumer,
	) (map[string]bool, error)
}

type diagnosticsClient interface {
	diagnosticsAPI
	Close() error
}

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
// Collection continues across independent artifacts and returns their failures.
func (manager *Manager) CollectDiagnostics(ctx context.Context, enclaveName, outputDir string) error {
	client, err := manager.newDiagnosticsClient()
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	return collectDiagnostics(ctx, client, enclaveName, outputDir)
}

func collectDiagnostics(ctx context.Context, client diagnosticsAPI, enclaveName, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create diagnostics directory: %w", err)
	}

	inspection, discoveredServices, inspectionErr := collectInspection(ctx, client, enclaveName, outputDir)
	// Partial inspection results remain usable when inspectionErr is non-nil.
	serviceDiagnostics, serviceLogsErr := collectServiceLogs(ctx, client, enclaveName, outputDir, discoveredServices)
	manifest := diagnosticsManifest{
		Enclave:    enclaveName,
		Inspection: inspection,
		Services:   serviceDiagnostics,
	}
	manifestErr := jsonfile.Write(filepath.Join(outputDir, "diagnostics.json"), manifest, "diagnostics manifest")

	return errors.Join(inspectionErr, serviceLogsErr, manifestErr)
}

func collectInspection(
	ctx context.Context,
	client diagnosticsAPI,
	enclaveName,
	outputDir string,
) (inspectionDiagnostic, []kurtosis.ServiceIdentity, error) {
	const file = "inspection.json"

	// Inspection may return useful partial metadata alongside an error.
	enclave, inspectErr := client.Inspect(ctx, enclaveName)
	if inspectErr != nil {
		inspectErr = fmt.Errorf("inspect Kurtosis enclave %s: %w", enclaveName, inspectErr)
	}
	writeErr := jsonfile.Write(
		filepath.Join(outputDir, file),
		enclave,
		"Kurtosis enclave inspection",
	)
	captureErr := errors.Join(inspectErr, writeErr)

	diagnostic := inspectionDiagnostic{
		File:     file,
		Captured: captureErr == nil,
	}
	if captureErr != nil {
		diagnostic.Error = captureErr.Error()
	}

	return diagnostic, enclave.Services, captureErr
}

func collectServiceLogs(
	ctx context.Context,
	client diagnosticsAPI,
	enclaveName,
	outputDir string,
	services []kurtosis.ServiceIdentity,
) ([]serviceDiagnostic, error) {
	if len(services) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "services"), 0o755); err != nil {
		return nil, fmt.Errorf("create service diagnostics directory: %w", err)
	}

	serviceUUIDs := make([]string, 0, len(services))
	outputsByUUID := make(map[string]*serviceLogOutput, len(services))
	for _, service := range services {
		serviceUUIDs = append(serviceUUIDs, service.UUID)
		outputsByUUID[service.UUID] = openServiceLog(outputDir, service)
	}

	notFound, streamErr := client.ServiceLogs(ctx, enclaveName, serviceUUIDs, func(uuid string, lines []string) {
		if output := outputsByUUID[uuid]; output != nil {
			output.writeLines(lines)
		}
	})
	if streamErr != nil {
		streamErr = fmt.Errorf("stream Kurtosis service logs for %s: %w", enclaveName, streamErr)
	}

	serviceDiagnostics := make([]serviceDiagnostic, 0, len(services))
	collectionErrors := []error{streamErr}
	for _, uuid := range serviceUUIDs {
		diagnostic, localErr := outputsByUUID[uuid].finalizeCapture(streamErr, notFound[uuid])
		serviceDiagnostics = append(serviceDiagnostics, diagnostic)
		collectionErrors = append(collectionErrors, localErr)
	}
	return serviceDiagnostics, errors.Join(collectionErrors...)
}

type serviceLogOutput struct {
	diagnostic serviceDiagnostic
	uuid       string
	path       string
	file       *os.File
	writer     *bufio.Writer
	outputErr  error
}

func openServiceLog(outputDir string, service kurtosis.ServiceIdentity) *serviceLogOutput {
	relativePath := filepath.Join("services", service.Name+".log")
	output := &serviceLogOutput{
		diagnostic: serviceDiagnostic{Name: service.Name, File: filepath.ToSlash(relativePath)},
		uuid:       service.UUID,
		path:       filepath.Join(outputDir, relativePath),
	}
	file, err := os.OpenFile(output.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		output.outputErr = fmt.Errorf("write diagnostic %s: %w", output.path, err)
		return output
	}
	output.file = file
	output.writer = bufio.NewWriter(file)
	return output
}

func (output *serviceLogOutput) writeLines(lines []string) {
	if output.outputErr != nil {
		return
	}
	for _, line := range lines {
		if _, err := output.writer.WriteString(line); err != nil {
			output.outputErr = fmt.Errorf("write diagnostic %s: %w", output.path, err)
			return
		}
		if err := output.writer.WriteByte('\n'); err != nil {
			output.outputErr = fmt.Errorf("write diagnostic %s: %w", output.path, err)
			return
		}
	}
}

func (output *serviceLogOutput) finalizeCapture(streamErr error, missing bool) (serviceDiagnostic, error) {
	var missingErr error
	if missing {
		missingErr = fmt.Errorf(
			"Kurtosis service logs not found for %s (%s)",
			output.diagnostic.Name,
			output.uuid,
		)
	}
	localErr := errors.Join(missingErr, output.closeOutput())
	captureErr := errors.Join(streamErr, localErr)
	output.diagnostic.Captured = captureErr == nil
	if captureErr != nil {
		output.diagnostic.Error = captureErr.Error()
	}
	// streamErr affects every manifest entry but belongs in the aggregate once.
	return output.diagnostic, localErr
}

func (output *serviceLogOutput) closeOutput() error {
	if output.file == nil {
		return output.outputErr
	}
	if output.outputErr == nil {
		if err := output.writer.Flush(); err != nil {
			output.outputErr = fmt.Errorf("write diagnostic %s: %w", output.path, err)
		}
	}
	if err := output.file.Close(); err != nil && output.outputErr == nil {
		output.outputErr = fmt.Errorf("write diagnostic %s: %w", output.path, err)
	}
	return output.outputErr
}
