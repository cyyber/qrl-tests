package devnet

import (
	"context"
	"errors"
	"fmt"

	"github.com/cyyber/qrl-tests/internal/dockerapi"
	dockerclient "github.com/moby/moby/client"
)

type servicePauser interface {
	containerLister
	ContainerPause(context.Context, string, dockerclient.ContainerPauseOptions) (dockerclient.ContainerPauseResult, error)
	ContainerUnpause(context.Context, string, dockerclient.ContainerUnpauseOptions) (dockerclient.ContainerUnpauseResult, error)
}

// PauseService freezes every process of a Kurtosis service's container until
// the returned resume runs. A paused service keeps its connections: what its
// peers and callers send waits in its sockets, and it handles that only once
// it resumes. resume must run even if the caller fails.
func PauseService(ctx context.Context, serviceID string) (func(context.Context) error, error) {
	client, err := dockerapi.New()
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	unpause, err := pauseService(ctx, client, serviceID)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	return func(ctx context.Context) error {
		return errors.Join(unpause(ctx), client.Close())
	}, nil
}

func pauseService(ctx context.Context, client servicePauser, serviceID string) (func(context.Context) error, error) {
	container, err := serviceContainer(ctx, client, serviceID)
	if err != nil {
		return nil, fmt.Errorf("find container: %w", err)
	}
	if _, err := client.ContainerPause(ctx, container.ID, dockerclient.ContainerPauseOptions{}); err != nil {
		return nil, fmt.Errorf("pause service %q: %w", serviceID, err)
	}
	return func(ctx context.Context) error {
		if _, err := client.ContainerUnpause(ctx, container.ID, dockerclient.ContainerUnpauseOptions{}); err != nil {
			return fmt.Errorf("resume service %q: %w", serviceID, err)
		}
		return nil
	}, nil
}
