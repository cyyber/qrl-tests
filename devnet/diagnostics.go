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
// Collection continues after individual failures and returns all encountered errors.
func (manager *Manager) CollectDiagnostics(ctx context.Context, enclaveName, outputDir string) error {
	client, err := manager.newDiagnosticsClient()
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	return manager.collectDiagnostics(ctx, client, enclaveName, outputDir)
}

func collectDiagnostics(ctx context.Context, api diagnosticsAPI, enclaveName, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create diagnostics directory: %w", err)
	}

	inspection, discoveredServices, inspectionErr := collectInspection(ctx, api, enclaveName, outputDir)
	// Partial inspection results remain usable when inspectionErr is non-nil.
	serviceDiagnostics, serviceLogsErr := collectServiceLogs(ctx, api, enclaveName, outputDir, discoveredServices)
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
	api diagnosticsAPI,
	enclaveName,
	outputDir string,
) (inspectionDiagnostic, []kurtosis.ServiceIdentity, error) {
	const file = "inspection.json"

	// Inspection may return useful partial metadata alongside an error.
	enclave, resultErr := api.Inspect(ctx, enclaveName)
	if resultErr != nil {
		resultErr = fmt.Errorf("inspect Kurtosis enclave %s: %w", enclaveName, resultErr)
	}
	if err := jsonfile.Write(
		filepath.Join(outputDir, file),
		enclave,
		"Kurtosis enclave inspection",
	); err != nil {
		resultErr = errors.Join(resultErr, err)
	}

	diagnostic := inspectionDiagnostic{
		File:     file,
		Captured: resultErr == nil,
	}
	if resultErr != nil {
		diagnostic.Error = resultErr.Error()
	}

	return diagnostic, enclave.Services, resultErr
}

func collectServiceLogs(
	ctx context.Context,
	api diagnosticsAPI,
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
	for _, service := range services {
		serviceUUIDs = append(serviceUUIDs, service.UUID)
	}
	outputs := make([]*serviceLogOutput, 0, len(services))
	outputsByUUID := make(map[string]*serviceLogOutput, len(services))
	nameCounts := make(map[string]int, len(services))
	for _, service := range services {
		nameCounts[service.Name]++
	}
	for _, service := range services {
		output := openServiceLog(outputDir, service, nameCounts[service.Name] > 1)
		outputs = append(outputs, output)
		outputsByUUID[service.UUID] = output
	}

	notFound, streamErr := api.ServiceLogs(ctx, enclaveName, serviceUUIDs, func(uuid string, lines []string) {
		output := outputsByUUID[uuid]
		if output == nil || output.writeErr != nil {
			return
		}
		for _, line := range lines {
			if _, err := output.writer.WriteString(line); err != nil {
				output.writeErr = fmt.Errorf("write diagnostic %s: %w", output.path, err)
				return
			}
			if err := output.writer.WriteByte('\n'); err != nil {
				output.writeErr = fmt.Errorf("write diagnostic %s: %w", output.path, err)
				return
			}
		}
	})
	if streamErr != nil {
		streamErr = fmt.Errorf("stream Kurtosis service logs for %s: %w", enclaveName, streamErr)
	}

	serviceDiagnostics := make([]serviceDiagnostic, 0, len(services))
	collectionErrors := []error{streamErr}
	for _, output := range outputs {
		writeErr := output.close()
		var notFoundErr error
		if notFound[output.uuid] {
			notFoundErr = fmt.Errorf(
				"Kurtosis service logs not found for %s (%s)",
				output.diagnostic.Name,
				output.uuid,
			)
		}
		captureErr := errors.Join(streamErr, notFoundErr, writeErr)
		output.diagnostic.Captured = captureErr == nil
		if captureErr != nil {
			output.diagnostic.Error = captureErr.Error()
		}
		serviceDiagnostics = append(serviceDiagnostics, output.diagnostic)
		collectionErrors = append(collectionErrors, notFoundErr, writeErr)
	}
	return serviceDiagnostics, errors.Join(collectionErrors...)
}

type serviceLogOutput struct {
	diagnostic serviceDiagnostic
	uuid       string
	path       string
	file       *os.File
	writer     *bufio.Writer
	writeErr   error
}

func openServiceLog(outputDir string, service kurtosis.ServiceIdentity, disambiguate bool) *serviceLogOutput {
	fileName := service.Name
	if disambiguate {
		fileName += "-" + service.UUID
	}
	relativePath := filepath.Join("services", fileName+".log")
	path := filepath.Join(outputDir, relativePath)
	output := &serviceLogOutput{
		diagnostic: serviceDiagnostic{Name: service.Name, File: filepath.ToSlash(relativePath)},
		uuid:       service.UUID,
		path:       path,
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		output.writeErr = fmt.Errorf("write diagnostic %s: %w", path, err)
		return output
	}
	output.file = file
	output.writer = bufio.NewWriter(file)
	return output
}

func (output *serviceLogOutput) close() error {
	if output.file == nil {
		return output.writeErr
	}
	flushErr := output.writer.Flush()
	if flushErr != nil {
		flushErr = fmt.Errorf("write diagnostic %s: %w", output.path, flushErr)
	}
	closeErr := output.file.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("write diagnostic %s: %w", output.path, closeErr)
	}
	output.writeErr = errors.Join(output.writeErr, flushErr, closeErr)
	return output.writeErr
}
