package health

import (
	"context"
	"fmt"
	"slices"
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
	Ledger              *Ledger
	ExecutionMetricsURL string

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
	if err := monitor.start(ctx, participant); err != nil {
		monitor.Close()
		return nil, err
	}
	return monitor, nil
}

func (monitor *Monitor) start(ctx context.Context, participant devnet.Participant) error {
	containers, err := ParticipantContainers(ctx, monitor.docker, participant)
	if err != nil {
		return err
	}
	monitor.execution, monitor.ExecutionMetricsURL = containers[0].Name, containers[0].MetricsURL

	if err := monitor.Ledger.Watch(ctx, containers...); err != nil {
		return err
	}

	if monitor.peers, err = WatchPeers(ctx, participant.Execution.WebSocketURL); err != nil {
		return err
	}

	monitor.firstSlot, err = monitor.beacon.HeadSlot(ctx)
	return err
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
		PeerDrops:   except(drops, ownPeers...),
		MissedSlots: missed,
		Slots:       head - monitor.firstSlot,
		FinalityLag: monitor.chain.EpochAt(time.Now()) - checkpoints.Finalized.Epoch,
	}, nil
}

// except returns the drops of peers other than the given ones.
func except(drops []PeerDrop, peers ...qnode.ID) []PeerDrop {
	return slices.DeleteFunc(slices.Clone(drops), func(drop PeerDrop) bool {
		return slices.Contains(peers, drop.Peer)
	})
}
