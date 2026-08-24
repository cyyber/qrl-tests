package devnet

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet/internal/kurtosis"
	"github.com/cyyber/qrl-tests/internal/testutil"
	"github.com/stretchr/testify/require"
)

var diagnosticServices = []kurtosis.ServiceIdentity{
	{Name: "run-generate-genesis", UUID: "aaaa", Status: "STOPPED", Ports: []string{"<none>"}},
	{Name: "clef-keystore-generation-el-clef-keystore", UUID: "bbbb"},
	{Name: "validator-key-generation-cl-validator-keystore", UUID: "cccc"},
	{Name: "el-1-gqrl-qrysm", UUID: "dddd"},
	{Name: "cl-1-qrysm-gqrl", UUID: "eeee"},
	{Name: "signer-clef", UUID: "ffff"},
	{Name: "vc-1-gqrl-qrysm", UUID: "0123"},
}

type fakeDiagnosticsClient struct {
	inspection  kurtosis.EnclaveInspection
	inspectErr  error
	logs        map[string][]string
	logsErr     error
	logCalls    int
	requested   []string
	enclaveName string
}

func (client *fakeDiagnosticsClient) Inspect(
	context.Context,
	string,
) (kurtosis.EnclaveInspection, error) {
	return client.inspection, client.inspectErr
}

func (client *fakeDiagnosticsClient) ServiceLogs(
	_ context.Context,
	enclaveName string,
	serviceUUIDs []string,
	consume kurtosis.ServiceLogConsumer,
) error {
	client.logCalls++
	client.enclaveName = enclaveName
	client.requested = append([]string(nil), serviceUUIDs...)
	for _, uuid := range serviceUUIDs {
		consume(uuid, client.logs[uuid])
	}
	return client.logsErr
}

func diagnosticInspection() kurtosis.EnclaveInspection {
	return kurtosis.EnclaveInspection{
		Name:         "qrl-tests-abi",
		UUID:         "enclave-uuid",
		Status:       "RUNNING",
		CreationTime: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
		Production:   true,
		Services:     diagnosticServices,
		FilesArtifacts: []kurtosis.FilesArtifactIdentity{
			{Name: "genesis", UUID: "artifact-uuid"},
		},
	}
}

func TestCollectDiagnostics(t *testing.T) {
	output := t.TempDir()
	logs := make(map[string][]string, len(diagnosticServices))
	for _, service := range diagnosticServices {
		logs[service.UUID] = []string{"captured " + service.Name}
	}
	logs["aaaa"] = []string{
		"starting genesis",
		`el_premine_addrs: {"seed":"0x010000abcd"}`,
		"genesis failed",
	}
	client := &fakeDiagnosticsClient{
		inspection: diagnosticInspection(),
		logs:       logs,
	}

	require.NoError(t, collectDiagnostics(t.Context(), client, "qrl-tests-abi", output))
	require.Equal(t, 1, client.logCalls, "all services should use one log stream")
	require.Equal(t, "qrl-tests-abi", client.enclaveName)
	require.Equal(t, []string{"aaaa", "bbbb", "cccc", "dddd", "eeee", "ffff", "0123"}, client.requested)

	inspection, err := os.ReadFile(filepath.Join(output, "inspect.txt"))
	require.NoError(t, err)
	require.Contains(t, string(inspection), "Name:\tqrl-tests-abi\n")
	require.Contains(t, string(inspection), "UUID:\tenclave-uuid\n")
	require.Contains(t, string(inspection), "Status:\tRUNNING\n")
	require.Contains(t, string(inspection), "Creation Time:\t2026-08-24T12:00:00Z\n")
	require.Contains(t, string(inspection), "Flags:\tproduction\n")
	require.Contains(t, string(inspection), "artifact-uuid\tgenesis\n")
	require.Contains(t, string(inspection), "aaaa\trun-generate-genesis\t<none>\tSTOPPED\n")

	genesisLog, err := os.ReadFile(filepath.Join(output, "services", "run-generate-genesis.log"))
	require.NoError(t, err)
	require.Equal(t, "starting genesis\nel_premine_addrs: {\"seed\":\"0x010000abcd\"}\ngenesis failed\n", string(genesisLog))

	executionLog, err := os.ReadFile(filepath.Join(output, "services", "el-1-gqrl-qrysm.log"))
	require.NoError(t, err)
	require.Equal(t, "captured el-1-gqrl-qrysm\n", string(executionLog))

	manifest := testutil.ReadJSON[diagnosticsManifest](t, filepath.Join(output, "diagnostics.json"))
	require.True(t, manifest.Inspection.Captured)
	require.Len(t, manifest.Services, 7)
	for _, service := range manifest.Services {
		require.True(t, service.Captured)
	}
}

func TestCollectDiagnosticsPartialFailures(t *testing.T) {
	output := t.TempDir()
	failedLog := filepath.Join(output, "services", "run-generate-genesis.log")
	require.NoError(t, os.MkdirAll(failedLog, 0o755))
	client := &fakeDiagnosticsClient{
		inspection: diagnosticInspection(),
		inspectErr: errors.New("inspect unavailable"),
		logs: map[string][]string{
			"bbbb": {"partial output"},
			"dddd": {"captured after failure"},
		},
		logsErr: errors.New("log stream reset"),
	}

	err := collectDiagnostics(t.Context(), client, "qrl-tests-abi", output)
	require.ErrorContains(t, err, "inspect Kurtosis enclave qrl-tests-abi: inspect unavailable")
	require.ErrorContains(t, err, "write diagnostic "+failedLog)
	require.ErrorContains(t, err, "stream Kurtosis service logs for qrl-tests-abi: log stream reset")

	partialLog, readErr := os.ReadFile(filepath.Join(output, "services", "clef-keystore-generation-el-clef-keystore.log"))
	require.NoError(t, readErr)
	require.Equal(t, "partial output\n", string(partialLog))
	require.FileExists(t, filepath.Join(output, "services", "el-1-gqrl-qrysm.log"),
		"a failing service write must not stop the remaining captures")

	manifest := testutil.ReadJSON[diagnosticsManifest](t, filepath.Join(output, "diagnostics.json"))
	require.Contains(t, manifest.Inspection.Error, "inspect unavailable")
	require.False(t, manifest.Services[0].Captured)
	require.Contains(t, manifest.Services[0].Error, "write diagnostic")
	for _, service := range manifest.Services {
		require.False(t, service.Captured, "a failed stream cannot produce an authoritative capture")
		require.Contains(t, service.Error, "log stream reset")
	}
}

func TestCollectDiagnosticsWithoutServices(t *testing.T) {
	client := new(fakeDiagnosticsClient)
	require.NoError(t, collectDiagnostics(t.Context(), client, "empty", t.TempDir()))
	require.Zero(t, client.logCalls)
}

func TestCollectDiagnosticsDuplicateServiceNames(t *testing.T) {
	services := []kurtosis.ServiceIdentity{
		{Name: "recreated", UUID: "old-uuid"},
		{Name: "recreated", UUID: "new-uuid"},
	}
	client := &fakeDiagnosticsClient{
		inspection: kurtosis.EnclaveInspection{Name: "test", Services: services},
		logs: map[string][]string{
			"old-uuid": {"old"},
			"new-uuid": {"new"},
		},
	}
	output := t.TempDir()
	require.NoError(t, collectDiagnostics(t.Context(), client, "test", output))
	require.FileExists(t, filepath.Join(output, "services", "recreated-old-uuid.log"))
	require.FileExists(t, filepath.Join(output, "services", "recreated-new-uuid.log"))
}
