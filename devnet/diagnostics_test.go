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
	{Name: "el-1-gqrl-qrysm", UUID: "dddd"},
}

type fakeDiagnosticsClient struct {
	inspection  kurtosis.EnclaveInspection
	inspectErr  error
	logs        map[string][]string
	notFound    map[string]bool
	logsErr     error
	logCalls    int
	requested   []string
	enclaveName string
}

type fakeDiagnosticsSession struct {
	fakeDiagnosticsClient
	closed bool
}

func (session *fakeDiagnosticsSession) Close() error {
	session.closed = true
	return nil
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
) (map[string]bool, error) {
	client.logCalls++
	client.enclaveName = enclaveName
	client.requested = append([]string(nil), serviceUUIDs...)
	for _, uuid := range serviceUUIDs {
		consume(uuid, client.logs[uuid])
	}
	return client.notFound, client.logsErr
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

func TestManagerCollectDiagnosticsClosesClient(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(t.Context(), contextKey{}, "sentinel")
	session := new(fakeDiagnosticsSession)
	manager := &Manager{
		newDiagnosticsClient: func() (diagnosticsSession, error) { return session, nil },
		collectDiagnostics: func(
			collectCtx context.Context,
			_ diagnosticsClient,
			_, _ string,
		) error {
			require.Equal(t, "sentinel", collectCtx.Value(contextKey{}))
			return errors.New("collection failed")
		},
	}

	err := manager.CollectDiagnostics(ctx, "test", t.TempDir())
	require.ErrorContains(t, err, "collection failed")
	require.True(t, session.closed)
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
	require.Equal(t, []string{"aaaa", "dddd"}, client.requested)

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
	require.Len(t, manifest.Services, 2)
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
			"dddd": {"captured after failure"},
		},
		logsErr: errors.New("log stream reset"),
	}

	err := collectDiagnostics(t.Context(), client, "qrl-tests-abi", output)
	require.ErrorContains(t, err, "inspect Kurtosis enclave qrl-tests-abi: inspect unavailable")
	require.ErrorContains(t, err, "write diagnostic "+failedLog)
	require.ErrorContains(t, err, "stream Kurtosis service logs for qrl-tests-abi: log stream reset")

	partialLog, readErr := os.ReadFile(filepath.Join(output, "services", "el-1-gqrl-qrysm.log"))
	require.NoError(t, readErr)
	require.Equal(t, "captured after failure\n", string(partialLog),
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

func TestCollectDiagnosticsMissingServiceLogs(t *testing.T) {
	services := []kurtosis.ServiceIdentity{
		{Name: "stopped", UUID: "stopped-uuid"},
		{Name: "running", UUID: "running-uuid"},
	}
	client := &fakeDiagnosticsClient{
		inspection: kurtosis.EnclaveInspection{Name: "test", Services: services},
		logs:       map[string][]string{"running-uuid": {"captured"}},
		notFound:   map[string]bool{"stopped-uuid": true},
	}
	output := t.TempDir()

	err := collectDiagnostics(t.Context(), client, "test", output)
	require.ErrorContains(t, err, "Kurtosis service logs not found for stopped (stopped-uuid)")

	manifest := testutil.ReadJSON[diagnosticsManifest](t, filepath.Join(output, "diagnostics.json"))
	require.False(t, manifest.Services[0].Captured)
	require.Contains(t, manifest.Services[0].Error, "service logs not found")
	require.True(t, manifest.Services[1].Captured)
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
