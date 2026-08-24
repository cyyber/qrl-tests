package devnet

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/devnet/internal/kurtosis"
	"github.com/cyyber/qrl-tests/internal/jsonfile"
)

type diagnosticsClient interface {
	Inspect(ctx context.Context, enclaveName string) (kurtosis.EnclaveInspection, error)
	ServiceLogs(
		ctx context.Context,
		enclaveName string,
		serviceUUIDs []string,
		consume kurtosis.ServiceLogConsumer,
	) error
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
	client, err := manager.newClient()
	if err != nil {
		return err
	}
	return manager.collectDiagnostics(ctx, client, enclaveName, outputDir)
}

func collectDiagnostics(ctx context.Context, client diagnosticsClient, enclaveName, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create diagnostics directory: %w", err)
	}

	inspection, services, inspectionErr := collectInspection(ctx, client, enclaveName, outputDir)
	serviceDiagnostics, servicesErr := collectServiceLogs(ctx, client, enclaveName, outputDir, services)
	manifest := diagnosticsManifest{
		Enclave:    enclaveName,
		Inspection: inspection,
		Services:   serviceDiagnostics,
	}
	manifestErr := jsonfile.Write(filepath.Join(outputDir, "diagnostics.json"), manifest, "diagnostics manifest")

	return errors.Join(inspectionErr, servicesErr, manifestErr)
}

func collectInspection(
	ctx context.Context,
	client diagnosticsClient,
	enclaveName,
	outputDir string,
) (inspectionDiagnostic, []kurtosis.ServiceIdentity, error) {
	inspection := inspectionDiagnostic{File: "inspect.txt"}

	enclave, inspectErr := client.Inspect(ctx, enclaveName)
	if inspectErr != nil {
		inspectErr = fmt.Errorf("inspect Kurtosis enclave %s: %w", enclaveName, inspectErr)
	}
	writeErr := writeDiagnostic(
		filepath.Join(outputDir, inspection.File),
		formatInspection(enclave),
	)
	captureErr := errors.Join(inspectErr, writeErr)

	inspection.Captured = captureErr == nil
	if captureErr != nil {
		inspection.Error = captureErr.Error()
	}

	return inspection, enclave.Services, captureErr
}

func formatInspection(enclave kurtosis.EnclaveInspection) string {
	var inspection strings.Builder
	fmt.Fprintf(&inspection, "Name:\t%s\nUUID:\t%s\nStatus:\t%s\n", enclave.Name, enclave.UUID, enclave.Status)
	if !enclave.CreationTime.IsZero() {
		fmt.Fprintf(&inspection, "Creation Time:\t%s\n", enclave.CreationTime.UTC().Format(time.RFC3339))
	}
	if enclave.Production {
		fmt.Fprintln(&inspection, "Flags:\tproduction")
	} else {
		fmt.Fprintln(&inspection, "Flags:")
	}
	fmt.Fprintln(&inspection, "Files Artifacts:")
	for _, artifact := range enclave.FilesArtifacts {
		fmt.Fprintf(&inspection, "%s\t%s\n", artifact.UUID, artifact.Name)
	}
	fmt.Fprintln(&inspection, "User Services:")
	fmt.Fprintln(&inspection, "UUID\tName\tPorts\tStatus")
	for _, service := range enclave.Services {
		fmt.Fprintf(
			&inspection,
			"%s\t%s\t%s\t%s\n",
			service.UUID,
			service.Name,
			strings.Join(service.Ports, ", "),
			service.Status,
		)
	}
	return inspection.String()
}

func collectServiceLogs(
	ctx context.Context,
	client diagnosticsClient,
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

	streamErr := client.ServiceLogs(ctx, enclaveName, serviceUUIDs, func(uuid string, lines []string) {
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
		captureErr := errors.Join(streamErr, writeErr)
		output.diagnostic.Captured = captureErr == nil
		if captureErr != nil {
			output.diagnostic.Error = captureErr.Error()
		}
		serviceDiagnostics = append(serviceDiagnostics, output.diagnostic)
		collectionErrors = append(collectionErrors, writeErr)
	}
	return serviceDiagnostics, errors.Join(collectionErrors...)
}

type serviceLogOutput struct {
	diagnostic serviceDiagnostic
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

func writeDiagnostic(path, output string) error {
	if err := os.WriteFile(path, []byte(output), 0o600); err != nil {
		return fmt.Errorf("write diagnostic %s: %w", path, err)
	}
	return nil
}
