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

// ParticipantContainers returns a devnet participant's execution container,
// with its metrics page, and its consensus and validator containers if it runs
// them.
func ParticipantContainers(ctx context.Context, docker ContainerReader, participant devnet.Participant) ([]Container, error) {
	execution := serviceContainer(participant.Execution.ServiceInfo)
	metrics, err := metricsURL(ctx, docker, execution, participantMetricsPort)
	if err != nil {
		return nil, err
	}

	containers := []Container{{Name: execution, MetricsURL: metrics}}
	for _, service := range []devnet.ServiceInfo{participant.Consensus.ServiceInfo, participant.Validator.ServiceInfo} {
		if service.Name != "" {
			containers = append(containers, Container{Name: serviceContainer(service)})
		}
	}
	return containers, nil
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
