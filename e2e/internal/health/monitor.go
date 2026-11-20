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

// Monitor follows a participant's containers, peers and chain during a suite.
type Monitor struct {
	containers *containerStates
	docker     dockerclient.APIClient
	peers      *PeerWatch
	beacon     *beacon.Client
	chain      chaininfo.Info
	firstSlot  uint64
}

// StartMonitor starts monitoring participant.
func StartMonitor(ctx context.Context, participant devnet.Participant, client *beacon.Client, chain chaininfo.Info) (*Monitor, error) {
	docker, err := dockerapi.New()
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}

	monitor := &Monitor{containers: &containerStates{docker: docker}, docker: docker, beacon: client, chain: chain}
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
	if err := monitor.containers.record(ctx, containers...); err != nil {
		return err
	}

	if monitor.peers, err = WatchPeers(ctx, participant.Execution.WebSocketURL); err != nil {
		return err
	}

	monitor.firstSlot, err = monitor.beacon.HeadSlot(ctx)
	return err
}

// Add records containers started later, such as a sync node, for the report.
func (monitor *Monitor) Add(ctx context.Context, containers ...Container) error {
	return monitor.containers.record(ctx, containers...)
}

// Retire takes a container's final reading before the suite removes it. Its
// problems stay in the report.
func (monitor *Monitor) Retire(ctx context.Context, name string) error {
	return monitor.containers.retire(ctx, name)
}

// Close releases the monitor's Docker client and peer watch.
func (monitor *Monitor) Close() {
	if monitor.peers != nil {
		monitor.peers.Close()
	}
	_ = monitor.docker.Close()
}

// Report is what a monitor has seen since it started.
type Report struct {
	// Problems are what went wrong in the containers: exits, restarts, dropped
	// or refused work, and trouble lines in logs.
	Problems []string
	// PeerDrops are the peers the execution node dropped.
	PeerDrops []PeerDrop
	// MissedSlots are the slots without a block, of the Slots due one since
	// the start.
	MissedSlots []uint64
	Slots       uint64
	// FinalityLag is how many epochs the finalized one is behind the clock's.
	FinalityLag uint64
}

// MaxFinalityLag is the most a healthy chain lags: it finalizes the epoch
// before the previous one, and the current one may have just begun.
const MaxFinalityLag = 3

func (report Report) String() string {
	return fmt.Sprintf("%d problems, %d peer drops, %d missed slots of %d, finality %d epochs behind",
		len(report.Problems), len(report.PeerDrops), len(report.MissedSlots), report.Slots, report.FinalityLag)
}

// Report returns what the monitor has seen. It leaves out the drops of the
// given peers, which the suite disconnects itself.
func (monitor *Monitor) Report(ctx context.Context, ownPeers ...qnode.ID) (Report, error) {
	// A node that is down fails the steps below, and its problems say why.
	problems, err := monitor.containers.problems(ctx)
	fail := func(err error) (Report, error) {
		if len(problems) > 0 {
			err = fmt.Errorf("%w; the containers had %q", err, problems)
		}
		return Report{}, err
	}
	if err != nil {
		return fail(err)
	}

	drops, err := monitor.peers.Drops(ctx)
	if err != nil {
		return fail(err)
	}

	head, err := monitor.beacon.HeadSlot(ctx)
	if err != nil {
		return fail(err)
	}

	// The clock's slot may still get its block; every slot before it is due one.
	now := time.Now()
	last := max(head+1, monitor.chain.SlotAt(now)) - 1
	missed, err := MissedSlots(ctx, monitor.beacon, monitor.firstSlot+1, last)
	if err != nil {
		return fail(err)
	}

	checkpoints, err := monitor.beacon.FinalityCheckpoints(ctx)
	if err != nil {
		return fail(err)
	}

	epoch := monitor.chain.EpochAt(now)
	return Report{
		Problems:    problems,
		PeerDrops:   except(drops, ownPeers...),
		MissedSlots: missed,
		Slots:       last - min(last, monitor.firstSlot),
		FinalityLag: epoch - min(epoch, checkpoints.Finalized.Epoch),
	}, nil
}

// except returns the drops of peers other than the given ones.
func except(drops []PeerDrop, peers ...qnode.ID) []PeerDrop {
	return slices.DeleteFunc(slices.Clone(drops), func(drop PeerDrop) bool {
		return slices.Contains(peers, drop.Peer)
	})
}
