package devnet

import (
	"context"
	"errors"
	"testing"

	"github.com/cyyber/qrl-tests/devnet/internal/kurtosis"
	"github.com/stretchr/testify/require"
)

type startClient struct {
	createErr   error
	runErr      error
	services    map[string]kurtosis.Service
	createdName string
	destroyed   bool
}

func (*startClient) EnclaveExists(context.Context, string) (bool, error) { return false, nil }

func (client *startClient) CreateEnclave(_ context.Context, name string) error {
	client.createdName = name
	return client.createErr
}

func (client *startClient) RunRemotePackage(context.Context, string, string, string) error {
	return client.runErr
}

func (client *startClient) Services(context.Context, string) (map[string]kurtosis.Service, error) {
	return client.services, nil
}

func (client *startClient) DestroyEnclave(context.Context, string) error {
	client.destroyed = true
	return nil
}

func startManager(client *startClient) *Manager {
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

func TestStartCleansCreatedEnclave(t *testing.T) {
	client := &startClient{runErr: errors.New("package failed")}

	_, err := startManager(client).Start(t.Context(), startOptions())
	require.ErrorContains(t, err, "package failed")
	require.True(t, client.destroyed)
}

func TestStartCreateFailureSkipsCleanup(t *testing.T) {
	client := &startClient{createErr: errors.New("create failed")}

	_, err := startManager(client).Start(t.Context(), startOptions())
	require.ErrorContains(t, err, "create failed")
	require.False(t, client.destroyed)
	require.Equal(t, "failed-start", client.createdName)
}

func TestStartDefaults(t *testing.T) {
	client := &startClient{services: map[string]kurtosis.Service{
		"el-1-gqrl-qrysm": service("el-1-gqrl-qrysm", "execution", map[string]uint16{"rpc": 3201, "ws": 3301}),
		"cl-1-qrysm-gqrl": service("cl-1-qrysm-gqrl", "beacon", map[string]uint16{"http": 4201}),
	}}
	options := startOptions()
	options.EnclaveName = ""
	options.Backend = ""

	environment, err := startManager(client).Start(t.Context(), options)
	require.NoError(t, err)
	require.Equal(t, DefaultEnclaveName, client.createdName)
	require.Equal(t, DefaultEnclaveName, environment.EnclaveName)
	require.Equal(t, BackendDocker, environment.Backend)
	require.False(t, client.destroyed)
}

func TestStopIsIdempotent(t *testing.T) {
	client := new(startClient)
	require.NoError(t, startManager(client).Stop(t.Context(), "missing"))
	require.False(t, client.destroyed)
}
