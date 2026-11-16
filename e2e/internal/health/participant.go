package health

import (
	"context"
	"slices"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/theQRL/go-qrl/p2p/qnode"
)

// participantMetricsPort is the port of a participant's go-qrl metrics page.
const participantMetricsPort = 9001

// ParticipantContainers returns a devnet participant's execution, consensus
// and validator containers, the execution one with its metrics page.
func ParticipantContainers(ctx context.Context, docker ContainerReader, participant devnet.Participant) ([]Container, error) {
	execution := serviceContainer(participant.Execution.ServiceInfo)
	metrics, err := MetricsURL(ctx, docker, execution, participantMetricsPort)
	if err != nil {
		return nil, err
	}
	return []Container{
		{Name: execution, Metrics: metrics},
		{Name: serviceContainer(participant.Consensus.ServiceInfo)},
		{Name: serviceContainer(participant.Validator.ServiceInfo)},
	}, nil
}

// Except returns the drops of peers other than the given ones, such as a
// probe the suite disconnects itself.
func Except(drops []PeerDrop, peers ...qnode.ID) []PeerDrop {
	return slices.DeleteFunc(slices.Clone(drops), func(drop PeerDrop) bool {
		return slices.Contains(peers, drop.Peer)
	})
}

// serviceContainer returns the Docker name of a Kurtosis service's container.
func serviceContainer(service devnet.ServiceInfo) string {
	return service.Name + "--" + service.ID
}
