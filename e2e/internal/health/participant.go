package health

import (
	"context"
	"fmt"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/internal/dockerapi"
	dockerclient "github.com/moby/moby/client"
)

const participantMetricsPort = 9001

// ParticipantContainers returns a devnet participant's containers. The first
// is the execution client's, with its metrics URL.
func ParticipantContainers(ctx context.Context, docker ContainerReader, participant devnet.Participant) ([]Container, error) {
	execution := kurtosisContainerName(participant.Execution.ServiceInfo)
	metrics, err := metricsURL(ctx, docker, execution, participantMetricsPort)
	if err != nil {
		return nil, err
	}

	containers := []Container{{Name: execution, MetricsURL: metrics}}
	for _, service := range []devnet.ServiceInfo{participant.Consensus.ServiceInfo, participant.Validator.ServiceInfo} {
		if service.Name != "" {
			containers = append(containers, Container{Name: kurtosisContainerName(service)})
		}
	}
	return containers, nil
}

func kurtosisContainerName(service devnet.ServiceInfo) string {
	return service.Name + "--" + service.ID
}

// PublishedPort returns the host port that a container's TCP port is
// published to.
func PublishedPort(ctx context.Context, docker ContainerReader, container string, port uint16) (string, error) {
	inspected, err := docker.ContainerInspect(ctx, container, dockerclient.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", container, err)
	}
	hostPort, err := dockerapi.HostPort(inspected.Container, port)
	if err != nil {
		return "", fmt.Errorf("%s: %w", container, err)
	}
	return hostPort, nil
}
