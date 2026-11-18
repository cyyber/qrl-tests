package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/internal/dockerapi"
	dockerclient "github.com/moby/moby/client"
	"github.com/theQRL/go-qrl/p2p/qnode"
)

// Monitor watches a devnet participant for the length of a suite: its
// containers, the peers its execution node drops, and its chain.
type Monitor struct {
	Ledger *Ledger
	// ExecutionMetrics is the URL of the execution node's metrics page.
	ExecutionMetrics string

	docker    dockerclient.APIClient
	peers     *PeerWatch
	beacon    *beacon.Client
	chain     chaininfo.Info
	execution string
	firstSlot uint64
}

// StartMonitor starts watching participant, whose beacon node is client.
func StartMonitor(ctx context.Context, participant devnet.Participant, client *beacon.Client, chain chaininfo.Info) (*Monitor, error) {
	docker, err := dockerapi.New()
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	monitor := &Monitor{Ledger: NewLedger(docker), docker: docker, beacon: client, chain: chain}
	containers, err := ParticipantContainers(ctx, docker, participant)
	if err == nil {
		monitor.execution, monitor.ExecutionMetrics = containers[0].Name, containers[0].Metrics
		err = monitor.Ledger.Watch(ctx, containers...)
	}
	if err == nil {
		monitor.peers, err = WatchPeers(ctx, participant.Execution.WebSocketURL)
	}
	if err == nil {
		monitor.firstSlot, err = client.HeadSlot(ctx)
	}
	if err != nil {
		monitor.Close()
		return nil, err
	}
	return monitor, nil
}

// Close stops the monitor.
func (monitor *Monitor) Close() {
	if monitor.peers != nil {
		monitor.peers.Close()
	}
	_ = monitor.docker.Close()
}

// MeterRise is the problem the ledger reports when one of the execution
// node's meters rises by count, for a suite that causes the rise itself.
func (monitor *Monitor) MeterRise(meter string, count int) string {
	return fmt.Sprintf("%s: %s rose by %d", monitor.execution, meter, count)
}

// Report is what a monitor has seen since it started.
type Report struct {
	// Problems are the ledger's: exits, restarts, dropped or refused work,
	// crashes and size refusals in logs.
	Problems []string
	// PeerDrops are the peers the execution node dropped.
	PeerDrops []PeerDrop
	// MissedSlots are the slots without a block, of Slots since the start.
	MissedSlots []uint64
	Slots       uint64
	// FinalityLag is how many epochs the finalized one is behind.
	FinalityLag uint64
}

// MaxFinalityLag is the lag of a healthy chain at most: it finalizes the
// epoch before the previous one, and the current one may have just begun.
const MaxFinalityLag = 3

func (report Report) String() string {
	return fmt.Sprintf("%d problems, %d peer drops, %d missed slots of %d, finality %d epochs behind",
		len(report.Problems), len(report.PeerDrops), len(report.MissedSlots), report.Slots, report.FinalityLag)
}

// Report returns what the monitor has seen. It leaves out the drops of the
// given peers, which the suite disconnects itself.
func (monitor *Monitor) Report(ctx context.Context, ownPeers ...qnode.ID) (Report, error) {
	problems, err := monitor.Ledger.Problems(ctx)
	if err != nil {
		return Report{}, err
	}
	drops, err := monitor.peers.Drops(ctx)
	if err != nil {
		return Report{}, err
	}
	head, err := monitor.beacon.HeadSlot(ctx)
	if err != nil {
		return Report{}, err
	}
	missed, err := MissedSlots(ctx, monitor.beacon, monitor.firstSlot+1, head)
	if err != nil {
		return Report{}, err
	}
	checkpoints, err := monitor.beacon.FinalityCheckpoints(ctx)
	if err != nil {
		return Report{}, err
	}
	return Report{
		Problems:    problems,
		PeerDrops:   Except(drops, ownPeers...),
		MissedSlots: missed,
		Slots:       head - monitor.firstSlot,
		FinalityLag: monitor.chain.EpochAt(time.Now()) - checkpoints.Finalized.Epoch,
	}, nil
}

// HeadVotes is how a slot's committee voted on the slot's own block.
type HeadVotes struct {
	Slot uint64
	// Voted is how many members' attestations are in a block, and ForBlock
	// how many of those name the slot's block as the head.
	Voted, ForBlock int
}

// SlotHeadVotes returns the head votes of each slot from first to last that
// has a block. A block that reached the committee late shows as votes for
// another head. The attestations of a slot are read from the blocks of the
// slots after it, up to the given number of them.
func SlotHeadVotes(ctx context.Context, client *beacon.Client, first, last, inclusionSlots uint64) ([]HeadVotes, error) {
	head, err := client.HeadSlot(ctx)
	if err != nil {
		return nil, err
	}
	attestations := make(map[uint64][]beacon.Attestation)
	roots := make(map[uint64]string)
	for slot := first; slot <= min(last+inclusionSlots, head); slot++ {
		blockID := strconv.FormatUint(slot, 10)
		header, err := client.BlockHeader(ctx, blockID)
		if beacon.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		roots[slot] = header.Root
		block, err := client.BlockOperations(ctx, blockID)
		if err != nil {
			return nil, err
		}
		for _, attestation := range block.Attestations {
			attestations[attestation.Data.Slot] = append(attestations[attestation.Data.Slot], attestation)
		}
	}

	var votes []HeadVotes
	for slot := first; slot <= last; slot++ {
		root, proposed := roots[slot]
		if !proposed {
			continue
		}
		voted, forBlock := make(map[uint64]bool), make(map[uint64]bool)
		for _, attestation := range attestations[slot] {
			for position := range uint64(len(attestation.AggregationBits) * 8) {
				if !attestation.Includes(position) {
					continue
				}
				voted[position] = true
				if attestation.Data.BeaconBlockRoot == root {
					forBlock[position] = true
				}
			}
		}
		votes = append(votes, HeadVotes{Slot: slot, Voted: len(voted), ForBlock: len(forBlock)})
	}
	return votes, nil
}

// StatsReader is the part of the Docker client MemoryUsage uses.
type StatsReader interface {
	ContainerStats(context.Context, string, dockerclient.ContainerStatsOptions) (dockerclient.ContainerStatsResult, error)
}

// MemoryUsage returns the bytes of memory a container uses, as docker stats
// shows them: without the page cache of files it has not touched lately, which
// grows with the files it writes and the kernel can take back.
func MemoryUsage(ctx context.Context, docker StatsReader, container string) (uint64, error) {
	result, err := docker.ContainerStats(ctx, container, dockerclient.ContainerStatsOptions{})
	if err != nil {
		return 0, fmt.Errorf("read the stats of %s: %w", container, err)
	}
	defer result.Body.Close()
	var stats struct {
		Memory struct {
			Usage uint64 `json:"usage"`
			Stats struct {
				// cgroup v2, and cgroup v1.
				InactiveFile      uint64 `json:"inactive_file"`
				TotalInactiveFile uint64 `json:"total_inactive_file"`
			} `json:"stats"`
		} `json:"memory_stats"`
	}
	if err := json.NewDecoder(result.Body).Decode(&stats); err != nil {
		return 0, fmt.Errorf("decode the stats of %s: %w", container, err)
	}
	if stats.Memory.Usage == 0 {
		return 0, errors.New("the stats of " + container + " have no memory usage")
	}
	inactive := max(stats.Memory.Stats.InactiveFile, stats.Memory.Stats.TotalInactiveFile)
	return stats.Memory.Usage - min(inactive, stats.Memory.Usage), nil
}
