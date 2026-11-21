package health

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/p2p/qnode"
)

// fakeMonitor monitors a fake container, node and beacon chain. It started at
// slot 2; the chain has blocks at 3 and 5, the clock is in slot 7 and epoch 3,
// and epoch 1 is finalized.
func fakeMonitor(t *testing.T) (*Monitor, *fakeDocker, *fakeAdmin) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/qrl/v1/beacon/headers/head", "/qrl/v1/beacon/headers/5":
			_, _ = writer.Write([]byte(`{"data":{"root":"0xab","header":{"message":{"slot":"5"}}}}`))
		case "/qrl/v1/beacon/headers/3":
			_, _ = writer.Write([]byte(`{"data":{"root":"0xab","header":{"message":{"slot":"3"}}}}`))
		case "/qrl/v1/beacon/states/head/finality_checkpoints":
			_, _ = writer.Write([]byte(`{"data":{"previous_justified":{"epoch":"0","root":"0x00"},"current_justified":{"epoch":"1","root":"0x00"},"finalized":{"epoch":"1","root":"0x00"}}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	client, err := beacon.New(server.URL)
	require.NoError(t, err)

	docker := &fakeDocker{state: containertypes.State{Running: true, StartedAt: "earlier"}}

	admin, watch := startPeerWatch(t)
	t.Cleanup(watch.Close)

	monitor := &Monitor{
		containers: &containerStates{docker: docker},
		peers:      watch,
		beacon:     client,
		chain: chaininfo.Info{
			GenesisTime:   time.Now().Add(-7*time.Hour - 30*time.Minute),
			SlotsPerEpoch: 2,
			SlotDuration:  time.Hour,
			EpochDuration: 2 * time.Hour,
		},
		firstSlot: 2,
	}
	require.NoError(t, monitor.AddContainers(t.Context(), Container{Name: "node"}))
	return monitor, docker, admin
}

func TestReport(t *testing.T) {
	monitor, docker, admin := fakeMonitor(t)
	own, other := qnode.ID{1}, qnode.ID{2}
	admin.emit(t, dropped(own, "requested"))
	admin.emit(t, dropped(other, "useless peer"))
	awaitDrops(t, monitor.peers, 2)
	docker.state.StartedAt = "later"

	report, err := monitor.Report(t.Context(), own)
	require.NoError(t, err)
	require.Equal(t, []string{"node: restarted"}, report.ContainerProblems)
	require.Len(t, report.PeerDrops, 1)
	require.Equal(t, other, report.PeerDrops[0].Peer)
	require.Equal(t, []uint64{4, 6}, report.MissedSlots, "up to the slot before the clock's")
	require.Equal(t, uint64(4), report.DueSlots)
	require.Equal(t, uint64(2), report.FinalityLag)
	require.Equal(t, "container problems: 1, peer drops: 1, missed slots: 2 of 4, finality lag: 2 epochs", report.String())
}

func TestReportErr(t *testing.T) {
	require.NoError(t, Report{DueSlots: 4, FinalityLag: maxFinalityLag}.Err())

	peer := qnode.ID{2}
	report := Report{
		ContainerProblems: []string{"node: restarted"},
		PeerDrops:         []PeerDrop{{Peer: peer, Reason: "useless peer"}},
		MissedSlots:       []uint64{4, 6},
		DueSlots:          4,
		FinalityLag:       maxFinalityLag + 1,
	}
	require.EqualError(t, report.Err(), "node: restarted; dropped peer "+peer.TerminalString()+
		": useless peer; missed slots [4 6] of 4; finality 4 epochs behind")
}

func TestReportFailure(t *testing.T) {
	t.Run("a down node and beacon node", func(t *testing.T) {
		monitor, docker, _ := fakeMonitor(t)
		docker.state.Running = false
		server := httptest.NewServer(http.NotFoundHandler())
		server.Close()
		client, err := beacon.New(server.URL)
		require.NoError(t, err)
		monitor.beacon = client

		_, err = monitor.Report(t.Context())
		require.ErrorContains(t, err, `the containers had ["node: exited with code 0"]`)
	})

	t.Run("an uninspectable container", func(t *testing.T) {
		monitor, docker, _ := fakeMonitor(t)
		docker.err = errors.New("gone")

		_, err := monitor.Report(t.Context())
		require.EqualError(t, err, "node: gone")
	})
}
