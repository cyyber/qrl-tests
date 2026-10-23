package soak

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/internal/dockerapi"
	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
)

// kurtosisServiceUUIDLabel is the label Kurtosis stamps on every service
// container with the service UUID that devnet.ServiceInfo.ID carries.
const kurtosisServiceUUIDLabel = "com.kurtosistech.guid"

// dockerStatsClient is the part of the Docker client the soak reads.
type dockerStatsClient interface {
	ContainerList(context.Context, dockerclient.ContainerListOptions) (dockerclient.ContainerListResult, error)
	ContainerStats(context.Context, string, dockerclient.ContainerStatsOptions) (dockerclient.ContainerStatsResult, error)
}

// participantContainer is one client of one participant, keyed by the
// Kurtosis service UUID.
type participantContainer struct {
	participant int
	service     string
	role        string
}

// DockerUsage reads what the kernel says each participant container uses,
// through the Docker Engine stats API: the same working set and CPU that
// cAdvisor would report.
type DockerUsage struct {
	client   dockerStatsClient
	services map[string]participantContainer
	close    func() error
}

// NewDockerUsage connects to the Docker host the enclave runs on.
func NewDockerUsage(environment devnet.Environment) (*DockerUsage, error) {
	client, err := dockerapi.New()
	if err != nil {
		return nil, err
	}
	usage := newDockerUsage(client, environment)
	usage.close = client.Close
	return usage, nil
}

func newDockerUsage(client dockerStatsClient, environment devnet.Environment) *DockerUsage {
	services := make(map[string]participantContainer)
	for _, participant := range environment.Participants {
		for role, info := range map[string]devnet.ServiceInfo{
			string(ClientExecution): participant.Execution.ServiceInfo,
			string(ClientConsensus): participant.Consensus.ServiceInfo,
			string(ClientValidator): participant.Validator.ServiceInfo,
		} {
			if info.ID != "" {
				services[info.ID] = participantContainer{participant: participant.Index, service: info.Name, role: role}
			}
		}
	}
	return &DockerUsage{client: client, services: services}
}

func (usage *DockerUsage) Close() error {
	if usage == nil || usage.close == nil {
		return nil
	}
	return usage.close()
}

// ContainerUsage takes one stats reading of every running participant
// container. The daemon spends a second on each to compute CPU, so they are
// read in parallel.
func (usage *DockerUsage) ContainerUsage(ctx context.Context) ([]ContainerSample, error) {
	listed, err := usage.client.ContainerList(ctx, dockerclient.ContainerListOptions{
		Filters: make(dockerclient.Filters).Add("label", kurtosisServiceUUIDLabel),
	})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	var (
		group   sync.WaitGroup
		mutex   sync.Mutex
		samples []ContainerSample
		errs    []error
	)
	for _, summary := range listed.Items {
		owner, known := usage.services[summary.Labels[kurtosisServiceUUIDLabel]]
		if !known {
			continue
		}
		group.Add(1)
		go func() {
			defer group.Done()
			sample, err := usage.read(ctx, summary.ID, owner)
			mutex.Lock()
			defer mutex.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", owner.service, err))
				return
			}
			samples = append(samples, sample)
		}()
	}
	group.Wait()
	sortContainerSamples(samples)
	return samples, errors.Join(errs...)
}

func (usage *DockerUsage) read(ctx context.Context, id string, owner participantContainer) (ContainerSample, error) {
	result, err := usage.client.ContainerStats(ctx, id, dockerclient.ContainerStatsOptions{IncludePreviousSample: true})
	if err != nil {
		return ContainerSample{}, err
	}
	defer result.Body.Close()
	var stats containertypes.StatsResponse
	if err := json.NewDecoder(result.Body).Decode(&stats); err != nil {
		return ContainerSample{}, fmt.Errorf("decode stats: %w", err)
	}
	return ContainerSample{
		Participant:     owner.participant,
		Service:         owner.service,
		Container:       owner.role,
		WorkingSetBytes: workingSet(stats.MemoryStats),
		CPUCores:        cpuCores(stats),
	}, nil
}

// workingSet is usage minus inactive page cache, as cAdvisor and the
// kubelet compute it: memory the kernel cannot reclaim without swapping.
// cgroup v2 reports inactive_file, cgroup v1 total_inactive_file.
func workingSet(memory containertypes.MemoryStats) float64 {
	inactive, found := memory.Stats["inactive_file"]
	if !found {
		inactive = memory.Stats["total_inactive_file"]
	}
	if inactive > memory.Usage {
		return 0
	}
	return float64(memory.Usage - inactive)
}

// cpuCores is the share of the host the container used between the
// daemon's two readings, in cores.
func cpuCores(stats containertypes.StatsResponse) float64 {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)
	if cpuDelta <= 0 || systemDelta <= 0 {
		return 0
	}
	online := float64(stats.CPUStats.OnlineCPUs)
	if online == 0 {
		online = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	return cpuDelta / systemDelta * online
}

func sortContainerSamples(samples []ContainerSample) {
	slices.SortFunc(samples, func(a, b ContainerSample) int {
		return cmp.Or(cmp.Compare(a.Participant, b.Participant), cmp.Compare(a.Container, b.Container))
	})
}
