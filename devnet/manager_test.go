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

func (*startClient) Services(context.Context, string) (map[string]kurtosis.Service, error) {
	return nil, nil
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

func TestStartDefaultsEnclaveName(t *testing.T) {
	client := &startClient{createErr: errors.New("create failed")}
	options := startOptions()
	options.EnclaveName = ""

	_, err := startManager(client).Start(t.Context(), options)
	require.ErrorContains(t, err, "create failed")
	require.Equal(t, DefaultEnclaveName, client.createdName)
}

func TestStopIsIdempotent(t *testing.T) {
	client := new(startClient)
	require.NoError(t, startManager(client).Stop(t.Context(), "missing"))
	require.False(t, client.destroyed)
}
