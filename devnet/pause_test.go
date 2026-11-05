package devnet

import (
	"context"
	"errors"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestPauseService(t *testing.T) {
	client := &fakePauser{fakeDocker: fakeDocker{containers: []containertypes.Summary{{ID: "beacon-container"}}}}

	resume, err := pauseService(t.Context(), client, "consensus-service")
	require.NoError(t, err)
	require.Equal(t, dockerclient.Filters{
		"label": {kurtosisServiceUUIDDockerLabel + "=consensus-service": true},
	}, client.listed.Filters)
	require.Equal(t, []string{"beacon-container"}, client.paused)
	require.Empty(t, client.unpaused)

	require.NoError(t, resume(t.Context()))
	require.Equal(t, []string{"beacon-container"}, client.unpaused)
}

func TestPauseServiceErrors(t *testing.T) {
	client := &fakePauser{}
	_, err := pauseService(t.Context(), client, "consensus-service")
	require.ErrorContains(t, err, `expected one running Docker container for service "consensus-service", found 0`)
	require.Empty(t, client.paused)

	client = &fakePauser{fakeDocker: fakeDocker{listErr: errors.New("daemon unavailable")}}
	_, err = pauseService(t.Context(), client, "consensus-service")
	require.EqualError(t, err, "find container: daemon unavailable")
	require.Empty(t, client.paused)

	client = &fakePauser{
		fakeDocker: fakeDocker{containers: []containertypes.Summary{{ID: "beacon-container"}}},
		pauseErr:   errors.New("already paused"),
	}
	resume, err := pauseService(t.Context(), client, "consensus-service")
	require.EqualError(t, err, `pause service "consensus-service": already paused`)
	require.Nil(t, resume)

	client = &fakePauser{
		fakeDocker: fakeDocker{containers: []containertypes.Summary{{ID: "beacon-container"}}},
		unpauseErr: errors.New("not paused"),
	}
	resume, err = pauseService(t.Context(), client, "consensus-service")
	require.NoError(t, err)
	require.EqualError(t, resume(t.Context()), `resume service "consensus-service": not paused`)
}

// fakePauser records the containers it pauses and resumes.
type fakePauser struct {
	fakeDocker
	pauseErr   error
	unpauseErr error
	paused     []string
	unpaused   []string
}

func (client *fakePauser) ContainerPause(_ context.Context, containerID string, _ dockerclient.ContainerPauseOptions) (dockerclient.ContainerPauseResult, error) {
	client.paused = append(client.paused, containerID)
	return dockerclient.ContainerPauseResult{}, client.pauseErr
}

func (client *fakePauser) ContainerUnpause(_ context.Context, containerID string, _ dockerclient.ContainerUnpauseOptions) (dockerclient.ContainerUnpauseResult, error) {
	client.unpaused = append(client.unpaused, containerID)
	return dockerclient.ContainerUnpauseResult{}, client.unpauseErr
}
