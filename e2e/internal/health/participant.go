package health

import (
	"context"
	"fmt"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/moby/moby/api/types/network"
	dockerclient "github.com/moby/moby/client"
)

// participantMetricsPort is the port of a participant's go-qrl metrics page.
const participantMetricsPort = 9001

// ParticipantContainers returns a devnet participant's execution, consensus
// and validator containers, the execution one with its metrics page.
func ParticipantContainers(ctx context.Context, docker ContainerReader, participant devnet.Participant) ([]Container, error) {
	execution := serviceContainer(participant.Execution.ServiceInfo)
	metrics, err := metricsURL(ctx, docker, execution, participantMetricsPort)
	if err != nil {
		return nil, err
	}
	return []Container{
		{Name: execution, MetricsURL: metrics},
		{Name: serviceContainer(participant.Consensus.ServiceInfo)},
		{Name: serviceContainer(participant.Validator.ServiceInfo)},
	}, nil
}

// serviceContainer returns the Docker name of a Kurtosis service's container.
func serviceContainer(service devnet.ServiceInfo) string {
	return service.Name + "--" + service.ID
}

// PublishedPort returns the host port that a container's TCP port is
// published to.
func PublishedPort(ctx context.Context, docker ContainerReader, container string, port uint16) (string, error) {
	inspected, err := docker.ContainerInspect(ctx, container, dockerclient.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", container, err)
	}
	published, _ := network.PortFrom(port, network.TCP)
	settings := inspected.Container.NetworkSettings
	if settings == nil || len(settings.Ports[published]) == 0 || settings.Ports[published][0].HostPort == "" {
		return "", fmt.Errorf("port %d of %s is not published", port, container)
	}
	return settings.Ports[published][0].HostPort, nil
}
