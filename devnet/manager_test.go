package devnet

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet/internal/kurtosis"
	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

type fakeEnclaveClient struct {
	exists         bool
	createErr      error
	runErr         error
	destroyErr     error
	services       map[string]kurtosis.Service
	createdName    string
	packageLocator string
	destroyed      bool
	onDestroy      func(context.Context)
	stopped        []string
	started        []string
	serviceErr     error
}

func (client *fakeEnclaveClient) EnclaveExists(context.Context, string) (bool, error) {
	return client.exists && !client.destroyed, nil
}

func (client *fakeEnclaveClient) CreateEnclave(_ context.Context, name string) error {
	client.createdName = name
	return client.createErr
}

func (client *fakeEnclaveClient) RunRemotePackage(_ context.Context, _, locator, _ string) error {
	client.packageLocator = locator
	return client.runErr
}

func (client *fakeEnclaveClient) Services(context.Context, string) (map[string]kurtosis.Service, error) {
	return client.services, nil
}

func (client *fakeEnclaveClient) StopServices(_ context.Context, _ string, names []string) error {
	client.stopped = append(client.stopped, names...)
	return client.serviceErr
}

func (client *fakeEnclaveClient) StartServices(_ context.Context, _ string, names []string) error {
	client.started = append(client.started, names...)
	return client.serviceErr
}

func (client *fakeEnclaveClient) DestroyEnclave(ctx context.Context, _ string) error {
	if client.onDestroy != nil {
		client.onDestroy(ctx)
	}
	if client.destroyErr != nil {
		return client.destroyErr
	}
	client.destroyed = true
	return nil
}

func testManager(client *fakeEnclaveClient) *Manager {
	return &Manager{
		newEnclaveClient: func() (enclaveClient, error) { return client, nil },
		newDiagnosticsClient: func() (diagnosticsClient, error) {
			panic("unexpected diagnostics client")
		},
		probe: func(context.Context, string, string) error { return nil },
	}
}

func useDiagnosticsClient(manager *Manager) *fakeDiagnosticsClient {
	client := new(fakeDiagnosticsClient)
	manager.newDiagnosticsClient = func() (diagnosticsClient, error) { return client, nil }
	return client
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

func TestStartCleansUpFailedEnclave(t *testing.T) {
	tests := []struct {
		name      string
		client    *fakeEnclaveClient
		wantError string
	}{
		{
			name:      "package run",
			client:    &fakeEnclaveClient{runErr: errors.New("package failed")},
			wantError: "run pinned qrl-package: package failed",
		},
		{
			name:      "endpoint resolution",
			client:    new(fakeEnclaveClient),
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

func TestStartDiagnosticsBeforeCleanup(t *testing.T) {
	client := &fakeEnclaveClient{runErr: errors.New("package failed")}
	manager := testManager(client)
	diagnostics := useDiagnosticsClient(manager)
	diagnosticsCalls := 0
	diagnostics.onInspect = func(_ context.Context, enclave string) {
		require.False(t, client.destroyed, "diagnostics must run before the enclave is destroyed")
		require.Equal(t, "failed-start", enclave)
		diagnosticsCalls++
	}

	diagnosticsDir := t.TempDir()
	options := startOptions()
	options.FailureDiagnosticsDir = diagnosticsDir
	_, err := manager.Start(t.Context(), options)
	require.ErrorContains(t, err, "package failed")
	require.Equal(t, 1, diagnosticsCalls)
	require.True(t, diagnostics.closed)
	require.FileExists(t, filepath.Join(diagnosticsDir, "diagnostics.json"))
	require.True(t, client.destroyed)
}

func TestStartJoinsDiagnosticsError(t *testing.T) {
	client := &fakeEnclaveClient{runErr: errors.New("package failed")}
	manager := testManager(client)
	diagnosticsErr := errors.New("logs unavailable")
	diagnostics := useDiagnosticsClient(manager)
	diagnostics.inspectErr = diagnosticsErr

	options := startOptions()
	options.FailureDiagnosticsDir = t.TempDir()
	_, err := manager.Start(t.Context(), options)
	require.ErrorIs(t, err, diagnosticsErr)
	require.ErrorContains(t, err, "package failed")
	require.ErrorContains(t, err, "collect start diagnostics: inspect Kurtosis enclave failed-start: logs unavailable")
	require.True(t, client.destroyed, "a diagnostics failure must not leak the enclave")
}

func TestStartJoinsDiagnosticsClientError(t *testing.T) {
	client := &fakeEnclaveClient{runErr: errors.New("package failed")}
	manager := testManager(client)
	manager.newDiagnosticsClient = func() (diagnosticsClient, error) {
		return nil, errors.New("engine unavailable")
	}

	options := startOptions()
	options.FailureDiagnosticsDir = t.TempDir()
	_, err := manager.Start(t.Context(), options)
	require.ErrorContains(t, err, "package failed")
	require.ErrorContains(t, err, "collect start diagnostics: engine unavailable")
	require.True(t, client.destroyed, "a diagnostics connection failure must not leak the enclave")
}

func TestStartRecoveryAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := &fakeEnclaveClient{services: singleParticipant()}
	destroyCalls := 0
	client.onDestroy = func(ctx context.Context) {
		requireLiveBoundedContext(t, ctx, startCleanupTimeout)
		destroyCalls++
	}
	manager := testManager(client)
	diagnostics := useDiagnosticsClient(manager)
	manager.probe = func(context.Context, string, string) error {
		cancel()
		return errors.New("not ready")
	}
	diagnosticsCalls := 0
	diagnostics.onInspect = func(ctx context.Context, _ string) {
		requireLiveBoundedContext(t, ctx, startDiagnosticsTimeout)
		diagnosticsCalls++
	}

	options := startOptions()
	options.FailureDiagnosticsDir = t.TempDir()
	_, err := manager.Start(ctx, options)
	require.ErrorContains(t, err, "wait for network readiness: not ready")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, diagnosticsCalls)
	require.Equal(t, 1, destroyCalls)
	require.True(t, client.destroyed)
}

func TestStartCreateFailureSkipsCleanup(t *testing.T) {
	client := &fakeEnclaveClient{createErr: errors.New("create failed")}
	options := startOptions()
	options.FailureDiagnosticsDir = t.TempDir()

	_, err := testManager(client).Start(t.Context(), options)
	require.ErrorContains(t, err, "create failed")
	require.False(t, client.destroyed)
	require.Equal(t, "failed-start", client.createdName)
	require.Empty(t, client.packageLocator)
}

func TestStartRecoveryFailures(t *testing.T) {
	startErr := errors.New("package failed")
	diagnosticsErr := errors.New("logs unavailable")
	cleanupErr := errors.New("destroy failed")
	client := &fakeEnclaveClient{
		runErr:     startErr,
		destroyErr: cleanupErr,
	}
	manager := testManager(client)
	diagnostics := useDiagnosticsClient(manager)
	diagnostics.inspectErr = diagnosticsErr

	options := startOptions()
	options.FailureDiagnosticsDir = t.TempDir()
	_, err := manager.Start(t.Context(), options)
	require.ErrorIs(t, err, startErr)
	require.ErrorIs(t, err, diagnosticsErr)
	require.ErrorIs(t, err, cleanupErr)
	require.ErrorContains(t, err, "collect start diagnostics: inspect Kurtosis enclave failed-start: logs unavailable")
	require.ErrorContains(t, err, "clean up failed network")
}

func TestStartDefaults(t *testing.T) {
	client := &fakeEnclaveClient{services: singleParticipant()}
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
	_, err := testManager(new(fakeEnclaveClient)).Inspect(t.Context(), "missing")
	require.ErrorContains(t, err, `network "missing" is not running`)

	client := &fakeEnclaveClient{exists: true, services: singleParticipant()}
	environment, err := testManager(client).Inspect(t.Context(), "running")
	require.NoError(t, err)
	require.Equal(t, "running", environment.EnclaveName)
	require.Empty(t, environment.Backend)
	require.Len(t, environment.Participants, 1)
}

func TestStartUsesPinnedPackage(t *testing.T) {
	client := &fakeEnclaveClient{services: singleParticipant()}

	_, err := testManager(client).Start(t.Context(), startOptions())
	require.NoError(t, err)
	require.Regexp(t, `^github\.com/cyyber/qrl-package@[0-9a-f]{40}$`, PackageLocator)
	require.Equal(t, PackageLocator, client.packageLocator)
}

func TestStartRejectsInvalidImages(t *testing.T) {
	client := new(fakeEnclaveClient)
	options := startOptions()
	options.Images.Consensus = "local/QRYSM-BEACON:devnet"

	_, err := testManager(client).Start(t.Context(), options)
	require.ErrorContains(t, err, "prepare qrl-package parameters")
	require.ErrorContains(t, err, "consensus image")
	require.Empty(t, client.createdName, "no enclave may be created for a rejected image")
}

func TestStop(t *testing.T) {
	missing := new(fakeEnclaveClient)
	require.NoError(t, testManager(missing).Stop(t.Context(), "missing"))
	require.False(t, missing.destroyed, "stopping an absent network must be a no-op")

	running := &fakeEnclaveClient{exists: true}
	require.NoError(t, testManager(running).Stop(t.Context(), "running"))
	require.True(t, running.destroyed)
}

// fakeContainerKiller runs one container per Kurtosis service UUID.
type fakeContainerKiller struct {
	containers map[string]string // container ID by service UUID
	killErr    error
	killed     []string
	signals    []string
}

func (client *fakeContainerKiller) ContainerList(_ context.Context, options dockerclient.ContainerListOptions) (dockerclient.ContainerListResult, error) {
	var result dockerclient.ContainerListResult
	for uuid, id := range client.containers {
		if options.Filters["label"][kurtosisServiceUUIDDockerLabel+"="+uuid] {
			result.Items = append(result.Items, containertypes.Summary{ID: id})
		}
	}
	return result, nil
}

func (client *fakeContainerKiller) ContainerKill(_ context.Context, id string, options dockerclient.ContainerKillOptions) (dockerclient.ContainerKillResult, error) {
	client.killed = append(client.killed, id)
	client.signals = append(client.signals, options.Signal)
	return dockerclient.ContainerKillResult{}, client.killErr
}

func TestKillServices(t *testing.T) {
	newNetwork := func() (*Manager, *fakeEnclaveClient, *fakeContainerKiller) {
		client := &fakeEnclaveClient{exists: true, services: map[string]kurtosis.Service{
			"vc-1": {UUID: "uuid-vc-1"},
			"cl-1": {UUID: "uuid-cl-1"},
			"el-1": {UUID: "uuid-el-1"},
		}}
		docker := &fakeContainerKiller{containers: map[string]string{
			"uuid-vc-1": "container-vc-1",
			"uuid-cl-1": "container-cl-1",
			"uuid-el-1": "container-el-1",
		}}
		return testManager(client), client, docker
	}

	t.Run("kills, then marks stopped", func(t *testing.T) {
		manager, client, docker := newNetwork()
		require.NoError(t, manager.killServices(t.Context(), docker, "network", []string{"vc-1", "cl-1", "el-1"}))
		require.Equal(t, []string{"container-vc-1", "container-cl-1", "container-el-1"}, docker.killed)
		require.Equal(t, []string{"SIGKILL", "SIGKILL", "SIGKILL"}, docker.signals)
		require.Equal(t, []string{"vc-1", "cl-1", "el-1"}, client.stopped)
	})

	t.Run("unknown service after a kill", func(t *testing.T) {
		manager, client, docker := newNetwork()
		require.EqualError(t, manager.killServices(t.Context(), docker, "network", []string{"vc-1", "vc-2"}),
			`network "network" has no service "vc-2"`)
		require.Equal(t, []string{"container-vc-1"}, docker.killed)
		require.Equal(t, []string{"vc-1"}, client.stopped)
	})

	t.Run("no running container", func(t *testing.T) {
		manager, client, docker := newNetwork()
		delete(docker.containers, "uuid-cl-1")
		require.EqualError(t, manager.killServices(t.Context(), docker, "network", []string{"cl-1"}),
			`find cl-1 container: expected one running Docker container for service "uuid-cl-1", found 0`)
		require.Empty(t, client.stopped)
	})

	t.Run("kill fails", func(t *testing.T) {
		manager, client, docker := newNetwork()
		docker.killErr = errors.New("daemon unavailable")
		require.EqualError(t, manager.killServices(t.Context(), docker, "network", []string{"el-1"}),
			"kill el-1: daemon unavailable")
		require.Empty(t, client.stopped)
	})

	t.Run("stop fails", func(t *testing.T) {
		manager, client, docker := newNetwork()
		client.serviceErr = errors.New("engine unavailable")
		require.EqualError(t, manager.killServices(t.Context(), docker, "network", []string{"el-1"}),
			"engine unavailable")
		require.Equal(t, []string{"container-el-1"}, docker.killed)
		require.Equal(t, []string{"el-1"}, client.stopped)
	})
}

func TestStopAndStartServices(t *testing.T) {
	client := &fakeEnclaveClient{exists: true}
	manager := testManager(client)

	require.NoError(t, manager.StopServices(t.Context(), "network", []string{"vc-1", "cl-1", "el-1"}))
	require.NoError(t, manager.StartServices(t.Context(), "network", []string{"el-1", "cl-1", "vc-1"}))
	require.Equal(t, []string{"vc-1", "cl-1", "el-1"}, client.stopped)
	require.Equal(t, []string{"el-1", "cl-1", "vc-1"}, client.started)

	client.serviceErr = errors.New("engine unavailable")
	require.ErrorContains(t, manager.StopServices(t.Context(), "network", []string{"el-1"}), "stop services [el-1]: engine unavailable")
	require.ErrorContains(t, manager.StartServices(t.Context(), "network", []string{"el-1"}), "start services [el-1]: engine unavailable")
}

func TestCheckReady(t *testing.T) {
	client := &fakeEnclaveClient{exists: true, services: singleParticipant()}
	manager := testManager(client)
	var probedURL string
	manager.probe = func(_ context.Context, rpcURL, _ string) error {
		probedURL = rpcURL
		return errors.New("chain does not advance")
	}

	environment, err := manager.Inspect(t.Context(), "network")
	require.NoError(t, err, "inspecting must not probe")
	require.Empty(t, probedURL)

	require.ErrorContains(t, manager.CheckReady(t.Context(), environment), "chain does not advance")
	require.Equal(t, environment.Participants[0].Execution.RPCURL, probedURL)
	require.ErrorContains(t, manager.CheckReady(t.Context(), Environment{}), "no participants")
}
