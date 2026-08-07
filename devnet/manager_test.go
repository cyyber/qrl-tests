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

func TestStartCleansCreatedEnclave(t *testing.T) {
	client := &fakeClient{runErr: errors.New("package failed")}

	_, err := testManager(client).Start(t.Context(), startOptions())
	require.ErrorContains(t, err, "package failed")
	require.True(t, client.destroyed)
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

func TestDefaultPackageLocatorIsPinned(t *testing.T) {
	require.Regexp(t, `^github\.com/.+/qrl-package@[0-9a-f]{40}$`, DefaultPackageLocator)
}

func TestParsePackageLocator(t *testing.T) {
	locator, err := ParsePackageLocator("")
	require.NoError(t, err)
	require.Equal(t, DefaultPackageLocator, locator)

	locator, err = ParsePackageLocator("  github.com/rgeraldes24/qrl-package@feature/vm64  ")
	require.NoError(t, err)
	require.Equal(t, "github.com/rgeraldes24/qrl-package@feature/vm64", locator)

	locator, err = ParsePackageLocator("github.com/cyyber/qrl-package")
	require.NoError(t, err)
	require.Equal(t, "github.com/cyyber/qrl-package", locator)

	for name, value := range map[string]string{
		"other host":     "gitlab.com/cyyber/qrl-package",
		"missing repo":   "github.com/qrl-package",
		"empty ref":      "github.com/cyyber/qrl-package@ ",
		"embedded space": "github.com/cyyber/qrl package",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParsePackageLocator(value)
			require.ErrorContains(t, err, "invalid qrl-package locator")
		})
	}
}

func TestStartRunsConfiguredPackageLocator(t *testing.T) {
	client := &fakeClient{services: singleParticipant()}
	options := startOptions()

	_, err := testManager(client).Start(t.Context(), options)
	require.NoError(t, err)
	require.Equal(t, DefaultPackageLocator, client.runLocator)

	client = &fakeClient{services: singleParticipant()}
	options.PackageLocator = "github.com/rgeraldes24/qrl-package@0000000000000000000000000000000000000000"
	_, err = testManager(client).Start(t.Context(), options)
	require.NoError(t, err)
	require.Equal(t, options.PackageLocator, client.runLocator)
}

func TestStartRejectsInvalidPackageLocator(t *testing.T) {
	client := &fakeClient{services: singleParticipant()}
	options := startOptions()
	options.PackageLocator = "example.com/not/qrl-package"

	_, err := testManager(client).Start(t.Context(), options)
	require.ErrorContains(t, err, "invalid qrl-package locator")
	require.Empty(t, client.createdName, "no enclave may be created for a rejected locator")
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
