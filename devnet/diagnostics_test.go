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

type fakeDiagnosticsAPI struct {
	inspection  kurtosis.EnclaveInspection
	inspectErr  error
	onInspect   func(context.Context, string)
	logs        map[string][]string
	notFound    map[string]bool
	logsErr     error
	logCalls    int
	requested   []string
	enclaveName string
}

type fakeDiagnosticsClient struct {
	fakeDiagnosticsAPI
	closed bool
}

func (client *fakeDiagnosticsClient) Close() error {
	client.closed = true
	return nil
}

func (api *fakeDiagnosticsAPI) Inspect(
	ctx context.Context,
	enclaveName string,
) (kurtosis.EnclaveInspection, error) {
	if api.onInspect != nil {
		api.onInspect(ctx, enclaveName)
	}
	return api.inspection, api.inspectErr
}

func (api *fakeDiagnosticsAPI) ServiceLogs(
	_ context.Context,
	enclaveName string,
	serviceUUIDs []string,
	consume kurtosis.ServiceLogConsumer,
) (map[string]bool, error) {
	api.logCalls++
	api.enclaveName = enclaveName
	api.requested = append([]string(nil), serviceUUIDs...)
	for _, uuid := range serviceUUIDs {
		if lines, ok := api.logs[uuid]; ok {
			consume(uuid, lines)
		}
	}
	return api.notFound, api.logsErr
}

func diagnosticInspection() kurtosis.EnclaveInspection {
	return kurtosis.EnclaveInspection{
		Name:         "qrl-tests-abi",
		UUID:         "enclave-uuid",
		Status:       "RUNNING",
		Mode:         "PRODUCTION",
		CreationTime: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
		Services: []kurtosis.ServiceIdentity{
			{Name: "run-generate-genesis", UUID: "aaaa", Status: "STOPPED", Ports: []string{"<none>"}},
			{Name: "el-1-gqrl-qrysm", UUID: "dddd"},
		},
	}
}

func TestManagerClosesDiagnosticsClient(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(t.Context(), contextKey{}, "sentinel")
	collectionErr := errors.New("collection failed")
	client := &fakeDiagnosticsClient{fakeDiagnosticsAPI: fakeDiagnosticsAPI{
		inspectErr: collectionErr,
		onInspect: func(collectCtx context.Context, enclaveName string) {
			require.Equal(t, "sentinel", collectCtx.Value(contextKey{}))
			require.Equal(t, "test", enclaveName)
		},
	}}
	manager := &Manager{
		newDiagnosticsClient: func() (diagnosticsClient, error) { return client, nil },
	}

	err := manager.CollectDiagnostics(ctx, "test", t.TempDir())
	require.ErrorIs(t, err, collectionErr)
	require.True(t, client.closed)
}

func TestCollectDiagnostics(t *testing.T) {
	output := t.TempDir()
	inspection := diagnosticInspection()
	client := &fakeDiagnosticsAPI{
		inspection: inspection,
		logs: map[string][]string{
			"aaaa": {
				"starting genesis",
				`el_premine_addrs: {"seed":"0x010000abcd"}`,
				"genesis failed",
			},
			"dddd": {"captured el-1-gqrl-qrysm"},
		},
	}

	require.NoError(t, collectDiagnostics(t.Context(), client, "qrl-tests-abi", output))
	require.Equal(t, 1, client.logCalls, "all services should use one log stream")
	require.Equal(t, "qrl-tests-abi", client.enclaveName)
	require.Equal(t, []string{"aaaa", "dddd"}, client.requested)

	capturedInspection := testutil.ReadJSON[kurtosis.EnclaveInspection](t, filepath.Join(output, "inspection.json"))
	require.Equal(t, inspection, capturedInspection)

	genesisLog, err := os.ReadFile(filepath.Join(output, "services", "run-generate-genesis.log"))
	require.NoError(t, err)
	require.Equal(t, "starting genesis\nel_premine_addrs: {\"seed\":\"0x010000abcd\"}\ngenesis failed\n", string(genesisLog))

	executionLog, err := os.ReadFile(filepath.Join(output, "services", "el-1-gqrl-qrysm.log"))
	require.NoError(t, err)
	require.Equal(t, "captured el-1-gqrl-qrysm\n", string(executionLog))

	manifest := testutil.ReadJSON[diagnosticsManifest](t, filepath.Join(output, "diagnostics.json"))
	require.Equal(t, diagnosticsManifest{
		Enclave:    "qrl-tests-abi",
		Inspection: inspectionDiagnostic{File: "inspection.json", Captured: true},
		Services: []serviceDiagnostic{
			{Name: "run-generate-genesis", File: "services/run-generate-genesis.log", Captured: true},
			{Name: "el-1-gqrl-qrysm", File: "services/el-1-gqrl-qrysm.log", Captured: true},
		},
	}, manifest)
}

func TestCollectDiagnosticsPartialFailures(t *testing.T) {
	output := t.TempDir()
	failedLog := filepath.Join(output, "services", "run-generate-genesis.log")
	require.NoError(t, os.MkdirAll(failedLog, 0o755))
	inspectErr := errors.New("inspect unavailable")
	streamErr := errors.New("log stream reset")
	inspection := diagnosticInspection()
	client := &fakeDiagnosticsAPI{
		inspection: inspection,
		inspectErr: inspectErr,
		logs: map[string][]string{
			"dddd": {"captured after failure"},
		},
		logsErr: streamErr,
	}

	err := collectDiagnostics(t.Context(), client, "qrl-tests-abi", output)
	require.ErrorIs(t, err, inspectErr)
	require.ErrorIs(t, err, streamErr)
	require.ErrorContains(t, err, "inspect Kurtosis enclave qrl-tests-abi: inspect unavailable")
	require.ErrorContains(t, err, "write diagnostic "+failedLog)
	require.ErrorContains(t, err, "stream Kurtosis service logs for qrl-tests-abi: log stream reset")

	capturedInspection := testutil.ReadJSON[kurtosis.EnclaveInspection](t, filepath.Join(output, "inspection.json"))
	require.Equal(t, inspection, capturedInspection)

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

func TestCollectDiagnosticsMissingLogs(t *testing.T) {
	services := []kurtosis.ServiceIdentity{
		{Name: "stopped", UUID: "stopped-uuid"},
		{Name: "running", UUID: "running-uuid"},
	}
	client := &fakeDiagnosticsAPI{
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
	inspection := kurtosis.EnclaveInspection{Name: "empty"}
	client := &fakeDiagnosticsAPI{inspection: inspection}
	output := t.TempDir()
	require.NoError(t, collectDiagnostics(t.Context(), client, "empty", output))
	require.Zero(t, client.logCalls)

	capturedInspection := testutil.ReadJSON[kurtosis.EnclaveInspection](t, filepath.Join(output, "inspection.json"))
	require.Equal(t, inspection, capturedInspection)
	manifest := testutil.ReadJSON[diagnosticsManifest](t, filepath.Join(output, "diagnostics.json"))
	require.Equal(t, diagnosticsManifest{
		Enclave:    "empty",
		Inspection: inspectionDiagnostic{File: "inspection.json", Captured: true},
	}, manifest)
}
