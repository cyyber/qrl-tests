package devnet

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet/internal/kurtosis"
	"github.com/stretchr/testify/require"
)

const failureDiagnosticsDir = "reports/lanes/execution-abi/diagnostics"

type fakeClient struct {
	exists         bool
	createErr      error
	runErr         error
	destroyErr     error
	services       map[string]kurtosis.Service
	createdName    string
	packageLocator string
	destroyed      bool
	onDestroy      func(context.Context)
}

func (client *fakeClient) EnclaveExists(context.Context, string) (bool, error) {
	return client.exists && !client.destroyed, nil
}

func (client *fakeClient) CreateEnclave(_ context.Context, name string) error {
	client.createdName = name
	return client.createErr
}

func (client *fakeClient) RunRemotePackage(_ context.Context, _, locator, _ string) error {
	client.packageLocator = locator
	return client.runErr
}

func (client *fakeClient) Services(context.Context, string) (map[string]kurtosis.Service, error) {
	return client.services, nil
}

func (client *fakeClient) DestroyEnclave(ctx context.Context, _ string) error {
	if client.onDestroy != nil {
		client.onDestroy(ctx)
	}
	if client.destroyErr != nil {
		return client.destroyErr
	}
	client.destroyed = true
	return nil
}

func testManager(client *fakeClient) *Manager {
	return &Manager{
		newClient: func() (kurtosisClient, error) { return client, nil },
		probe:     func(context.Context, string, string) error { return nil },
		collectDiagnostics: func(context.Context, string, string) error {
			panic("unexpected diagnostics collection")
		},
	}
}

func startOptions() StartOptions {
	return StartOptions{
		EnclaveName: "failed-start",
		Images:      Images{Execution: "go-qrl:test"},
		Profile:     ProfileSingle,
	}
}

func singleParticipant() map[string]kurtosis.Service {
	return map[string]kurtosis.Service{
		"el-1-gqrl-qrysm": service("el-1-gqrl-qrysm", "execution", map[string]uint16{"rpc": 3201, "ws": 3301}),
		"cl-1-qrysm-gqrl": service("cl-1-qrysm-gqrl", "beacon", map[string]uint16{"http": 4201}),
	}
}

func requireLiveBoundedContext(t *testing.T, ctx context.Context, timeout time.Duration) {
	t.Helper()
	require.NoError(t, ctx.Err())
	deadline, ok := ctx.Deadline()
	require.True(t, ok, "recovery context must have a deadline")
	remaining := time.Until(deadline)
	require.Positive(t, remaining)
	require.LessOrEqual(t, remaining, timeout)
}

func TestStartCleansUpEnclaveAfterPostCreateFailure(t *testing.T) {
	tests := []struct {
		name      string
		client    *fakeClient
		wantError string
	}{
		{
			name:      "package run",
			client:    &fakeClient{runErr: errors.New("package failed")},
			wantError: "run pinned qrl-package: package failed",
		},
		{
			name:      "endpoint resolution",
			client:    new(fakeClient),
			wantError: "resolve network endpoints: no qrl-package participants found",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := testManager(test.client).Start(t.Context(), startOptions())
			require.ErrorContains(t, err, test.wantError)
			require.True(t, test.client.destroyed)
		})
	}
}

func TestStartCollectsDiagnosticsBeforeCleanup(t *testing.T) {
	client := &fakeClient{runErr: errors.New("package failed")}
	manager := testManager(client)
	diagnosticsCalls := 0
	manager.collectDiagnostics = func(_ context.Context, enclave, outputDir string) error {
		require.False(t, client.destroyed, "diagnostics must run before the enclave is destroyed")
		require.Equal(t, "failed-start", enclave)
		require.Equal(t, failureDiagnosticsDir, outputDir)
		diagnosticsCalls++
		return nil
	}

	options := startOptions()
	options.FailureDiagnosticsDir = failureDiagnosticsDir
	_, err := manager.Start(t.Context(), options)
	require.ErrorContains(t, err, "package failed")
	require.Equal(t, 1, diagnosticsCalls)
	require.True(t, client.destroyed)
}

func TestStartReportsDiagnosticsFailureAlongsideCause(t *testing.T) {
	client := &fakeClient{runErr: errors.New("package failed")}
	manager := testManager(client)
	manager.collectDiagnostics = func(context.Context, string, string) error {
		return errors.New("logs unavailable")
	}

	options := startOptions()
	options.FailureDiagnosticsDir = failureDiagnosticsDir
	_, err := manager.Start(t.Context(), options)
	require.ErrorContains(t, err, "package failed")
	require.ErrorContains(t, err, "collect start diagnostics: logs unavailable")
	require.True(t, client.destroyed, "a diagnostics failure must not leak the enclave")
}

func TestStartRecoveryUsesLiveBoundedContextsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := &fakeClient{services: singleParticipant()}
	destroyCalls := 0
	client.onDestroy = func(ctx context.Context) {
		requireLiveBoundedContext(t, ctx, startCleanupTimeout)
		destroyCalls++
	}
	manager := testManager(client)
	manager.probe = func(context.Context, string, string) error {
		cancel()
		return errors.New("not ready")
	}
	diagnosticsCalls := 0
	manager.collectDiagnostics = func(ctx context.Context, _, _ string) error {
		requireLiveBoundedContext(t, ctx, startDiagnosticsTimeout)
		diagnosticsCalls++
		return nil
	}

	options := startOptions()
	options.FailureDiagnosticsDir = failureDiagnosticsDir
	_, err := manager.Start(ctx, options)
	require.ErrorContains(t, err, "wait for network readiness: not ready")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, diagnosticsCalls)
	require.Equal(t, 1, destroyCalls)
	require.True(t, client.destroyed)
}

func TestStartCreateFailureSkipsCleanup(t *testing.T) {
	client := &fakeClient{createErr: errors.New("create failed")}
	options := startOptions()
	options.FailureDiagnosticsDir = failureDiagnosticsDir

	_, err := testManager(client).Start(t.Context(), options)
	require.ErrorContains(t, err, "create failed")
	require.False(t, client.destroyed)
	require.Equal(t, "failed-start", client.createdName)
	require.Empty(t, client.packageLocator)
}

func TestStartReportsCleanupFailure(t *testing.T) {
	client := &fakeClient{
		runErr:     errors.New("package failed"),
		destroyErr: errors.New("destroy failed"),
	}

	_, err := testManager(client).Start(t.Context(), startOptions())
	require.ErrorContains(t, err, "package failed")
	require.ErrorContains(t, err, "clean up failed network")
	require.ErrorContains(t, err, "destroy failed")
}

func TestStartDefaults(t *testing.T) {
	client := &fakeClient{services: singleParticipant()}
	options := startOptions()
	options.EnclaveName = ""
	options.Backend = ""
	options.Profile = ""

	environment, err := testManager(client).Start(t.Context(), options)
	require.NoError(t, err)
	require.Equal(t, DefaultEnclaveName, client.createdName)
	require.Equal(t, DefaultEnclaveName, environment.EnclaveName)
	require.Equal(t, BackendDocker, environment.Backend)
	require.False(t, client.destroyed)
}

func TestInspect(t *testing.T) {
	_, err := testManager(new(fakeClient)).Inspect(t.Context(), "missing")
	require.ErrorContains(t, err, `network "missing" is not running`)

	client := &fakeClient{exists: true, services: singleParticipant()}
	environment, err := testManager(client).Inspect(t.Context(), "running")
	require.NoError(t, err)
	require.Equal(t, "running", environment.EnclaveName)
	require.Empty(t, environment.Backend)
	require.Len(t, environment.Participants, 1)
}

func TestStartUsesPinnedPackage(t *testing.T) {
	client := &fakeClient{services: singleParticipant()}

	_, err := testManager(client).Start(t.Context(), startOptions())
	require.NoError(t, err)
	require.Regexp(t, `^github\.com/cyyber/qrl-package@[0-9a-f]{40}$`, PackageLocator)
	require.Equal(t, PackageLocator, client.packageLocator)
}

func TestStartRejectsInvalidImages(t *testing.T) {
	client := new(fakeClient)
	options := startOptions()
	options.Images.Consensus = "local/QRYSM-BEACON:devnet"

	_, err := testManager(client).Start(t.Context(), options)
	require.ErrorContains(t, err, "prepare qrl-package parameters")
	require.ErrorContains(t, err, "consensus image")
	require.Empty(t, client.createdName, "no enclave may be created for a rejected image")
}

func TestStop(t *testing.T) {
	missing := new(fakeClient)
	require.NoError(t, testManager(missing).Stop(t.Context(), "missing"))
	require.False(t, missing.destroyed, "stopping an absent network must be a no-op")

	running := &fakeClient{exists: true}
	require.NoError(t, testManager(running).Stop(t.Context(), "running"))
	require.True(t, running.destroyed)
}
