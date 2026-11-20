package health

import (
	"testing"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/stretchr/testify/require"
)

func TestParticipantContainers(t *testing.T) {
	docker := &fakeDocker{hostPort: "32000"}
	participant := devnet.Participant{
		Execution: devnet.ExecutionService{ServiceInfo: devnet.ServiceInfo{Name: "el-1", ID: "a"}},
		Consensus: devnet.ConsensusService{ServiceInfo: devnet.ServiceInfo{Name: "cl-1", ID: "b"}},
	}

	containers, err := ParticipantContainers(t.Context(), docker, participant)
	require.NoError(t, err)
	require.Equal(t, []Container{
		{Name: "el-1--a", MetricsURL: "http://127.0.0.1:32000/debug/metrics/prometheus"},
		{Name: "cl-1--b"},
	}, containers, "no validator container")

	docker.hostPort = ""
	_, err = ParticipantContainers(t.Context(), docker, participant)
	require.ErrorContains(t, err, "el-1--a: port 9001 is not published")
}
