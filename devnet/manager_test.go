package devnet

import (
	"context"
	"errors"
	"testing"

	"github.com/cyyber/qrl-tests/devnet/internal/kurtosis"
	"github.com/stretchr/testify/require"
)

type fakeClient struct {
	exists      bool
	createErr   error
	runErr      error
	destroyErr  error
	services    map[string]kurtosis.Service
	createdName string
	runLocator  string
	destroyed   bool
}

func (client *fakeClient) EnclaveExists(context.Context, string) (bool, error) {
	return client.exists && !client.destroyed, nil
}

func (client *fakeClient) CreateEnclave(_ context.Context, name string) error {
	client.createdName = name
	return client.createErr
}

func (client *fakeClient) RunRemotePackage(_ context.Context, _ string, locator string, _ string) error {
	client.runLocator = locator
	return client.runErr
}

func (client *fakeClient) Services(context.Context, string) (map[string]kurtosis.Service, error) {
	return client.services, nil
}

func (client *fakeClient) DestroyEnclave(context.Context, string) error {
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
		collect: func(context.Context, Backend, string, string) error {
			return errors.New("no diagnostics were requested")
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

func TestStartCleansUpEveryPostCreateFailure(t *testing.T) {
	tests := []struct {
		name      string
		client    *fakeClient
		configure func(*testing.T, *Manager) context.Context
		wantError string
	}{
		{
			name:      "package run",
			client:    &fakeClient{runErr: errors.New("package failed")},
			wantError: "run qrl-package: package failed",
		},
		{
			name:      "endpoint resolution",
			client:    new(fakeClient),
			wantError: "resolve network endpoints: no qrl-package participants found",
		},
		{
			name:   "readiness",
			client: &fakeClient{services: singleParticipant()},
			configure: func(t *testing.T, manager *Manager) context.Context {
				manager.probe = func(context.Context, string, string) error {
					return errors.New("not ready")
				}
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
			wantError: "wait for network readiness: not ready",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := testManager(test.client)
			ctx := t.Context()
			if test.configure != nil {
				ctx = test.configure(t, manager)
			}

			_, err := manager.Start(ctx, startOptions())
			require.ErrorContains(t, err, test.wantError)
			require.True(t, test.client.destroyed)
		})
	}
}

func TestStartCollectsDiagnosticsBeforeCleanup(t *testing.T) {
	client := &fakeClient{runErr: errors.New("package failed")}
	manager := testManager(client)
	var order []string
	manager.collect = func(_ context.Context, backend Backend, enclave, outputDir string) error {
		require.False(t, client.destroyed, "diagnostics must run before the enclave is destroyed")
		require.Equal(t, "failed-start", enclave)
		require.Equal(t, "reports/diagnostics/execution-abi", outputDir)
		order = append(order, "collect")
		return nil
	}

	options := startOptions()
	options.FailureDiagnosticsDir = "reports/diagnostics/execution-abi"
	_, err := manager.Start(t.Context(), options)
	require.ErrorContains(t, err, "package failed")
	require.Equal(t, []string{"collect"}, order)
	require.True(t, client.destroyed)
}

func TestStartReportsDiagnosticsFailureAlongsideCause(t *testing.T) {
	client := &fakeClient{runErr: errors.New("package failed")}
	manager := testManager(client)
	manager.collect = func(context.Context, Backend, string, string) error {
		return errors.New("dump broke")
	}

	options := startOptions()
	options.FailureDiagnosticsDir = "reports/diagnostics/execution-abi"
	_, err := manager.Start(t.Context(), options)
	require.ErrorContains(t, err, "package failed")
	require.ErrorContains(t, err, "collect start diagnostics: dump broke")
	require.True(t, client.destroyed, "a diagnostics failure must not leak the enclave")
}

func TestStartCreateFailureSkipsCleanup(t *testing.T) {
	client := &fakeClient{createErr: errors.New("create failed")}

	_, err := testManager(client).Start(t.Context(), startOptions())
	require.ErrorContains(t, err, "create failed")
	require.False(t, client.destroyed)
	require.Equal(t, "failed-start", client.createdName)
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

func TestPackageLocatorIsPinned(t *testing.T) {
	require.Regexp(t, `^github\.com/.+/qrl-package@[0-9a-f]{40}$`, PackageLocator)
}

func TestStartRunsPinnedPackageLocator(t *testing.T) {
	client := &fakeClient{services: singleParticipant()}

	_, err := testManager(client).Start(t.Context(), startOptions())
	require.NoError(t, err)
	require.Equal(t, PackageLocator, client.runLocator)
}

func TestStartRejectsInvalidImages(t *testing.T) {
	client := &fakeClient{services: singleParticipant()}
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
