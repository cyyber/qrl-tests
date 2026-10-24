package soak

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/cyyber/qrl-tests/devnet"
	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

type fakeDockerStats struct {
	containers []containertypes.Summary
	stats      map[string]containertypes.StatsResponse
	filters    dockerclient.Filters
}

func (fake *fakeDockerStats) ContainerList(_ context.Context, options dockerclient.ContainerListOptions) (dockerclient.ContainerListResult, error) {
	fake.filters = options.Filters
	return dockerclient.ContainerListResult{Items: fake.containers}, nil
}

func (fake *fakeDockerStats) ContainerStats(_ context.Context, id string, options dockerclient.ContainerStatsOptions) (dockerclient.ContainerStatsResult, error) {
	if !options.IncludePreviousSample {
		return dockerclient.ContainerStatsResult{}, io.ErrUnexpectedEOF
	}
	payload, err := json.Marshal(fake.stats[id])
	if err != nil {
		return dockerclient.ContainerStatsResult{}, err
	}
	return dockerclient.ContainerStatsResult{Body: io.NopCloser(strings.NewReader(string(payload)))}, nil
}

func TestDockerContainerUsage(t *testing.T) {
	environment := devnet.Environment{Participants: []devnet.Participant{{
		Index:     2,
		Execution: devnet.ExecutionService{ServiceInfo: devnet.ServiceInfo{Name: "el-2-gqrl-qrysm", ID: "el-uuid"}},
		Consensus: devnet.ConsensusService{ServiceInfo: devnet.ServiceInfo{Name: "cl-2-qrysm-gqrl", ID: "cl-uuid"}},
	}}}
	fake := &fakeDockerStats{
		containers: []containertypes.Summary{
			{ID: "c-cl", Labels: map[string]string{kurtosisServiceUUIDLabel: "cl-uuid"}},
			{ID: "c-el", Labels: map[string]string{kurtosisServiceUUIDLabel: "el-uuid"}},
			{ID: "c-prometheus", Labels: map[string]string{kurtosisServiceUUIDLabel: "prometheus-uuid"}},
		},
		stats: map[string]containertypes.StatsResponse{
			"c-el": {
				MemoryStats: containertypes.MemoryStats{Usage: 3 << 30, Stats: map[string]uint64{"inactive_file": 1 << 30}},
				CPUStats: containertypes.CPUStats{
					CPUUsage: containertypes.CPUUsage{TotalUsage: 3_000}, SystemUsage: 10_000, OnlineCPUs: 4,
				},
				PreCPUStats: containertypes.CPUStats{
					CPUUsage: containertypes.CPUUsage{TotalUsage: 1_000}, SystemUsage: 6_000,
				},
			},
			"c-cl": {
				MemoryStats: containertypes.MemoryStats{Usage: 1 << 30, Stats: map[string]uint64{"total_inactive_file": 256 << 20}},
			},
		},
	}

	samples, err := newDockerUsage(fake, environment).ContainerUsage(t.Context())
	require.NoError(t, err)
	require.Equal(t, map[string]bool{kurtosisServiceUUIDLabel: true}, fake.filters["label"])
	require.Equal(t, []ContainerSample{
		{Participant: 2, Service: "cl-2-qrysm-gqrl", Container: "consensus", WorkingSetBytes: 768 << 20},
		{Participant: 2, Service: "el-2-gqrl-qrysm", Container: "execution", WorkingSetBytes: 2 << 30, CPUCores: 2},
	}, samples)
}

func TestWorkingSetNeverNegative(t *testing.T) {
	require.Zero(t, workingSet(containertypes.MemoryStats{Usage: 1, Stats: map[string]uint64{"inactive_file": 2}}))
}
