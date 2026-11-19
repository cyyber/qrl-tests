package health

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

// fakeDocker serves one container, which publishes port (6060 when zero) on
// hostPort and has no state when stateless. It records the logs' Since.
type fakeDocker struct {
	mutex     sync.Mutex
	state     containertypes.State
	stateless bool
	restarts  int
	port      uint16
	hostPort  string
	logs      string
	since     string
}

func (docker *fakeDocker) ContainerInspect(context.Context, string, dockerclient.ContainerInspectOptions) (dockerclient.ContainerInspectResult, error) {
	docker.mutex.Lock()
	defer docker.mutex.Unlock()
	var state *containertypes.State
	if !docker.stateless {
		state = new(docker.state)
	}
	port, _ := network.PortFrom(cmp.Or(docker.port, 6060), network.TCP)
	return dockerclient.ContainerInspectResult{Container: containertypes.InspectResponse{
		State:        state,
		RestartCount: docker.restarts,
		NetworkSettings: &containertypes.NetworkSettings{
			Ports: network.PortMap{port: {{HostIP: netip.IPv4Unspecified(), HostPort: docker.hostPort}}},
		},
	}}, nil
}

func (docker *fakeDocker) ContainerLogs(_ context.Context, _ string, options dockerclient.ContainerLogsOptions) (dockerclient.ContainerLogsResult, error) {
	docker.mutex.Lock()
	defer docker.mutex.Unlock()
	docker.since = options.Since
	// Docker frames each write with its stream and length.
	frame := make([]byte, 8, 8+len(docker.logs))
	frame[0] = 1
	binary.BigEndian.PutUint32(frame[4:], uint32(len(docker.logs)))
	return io.NopCloser(bytes.NewReader(append(frame, docker.logs...))), nil
}

func (docker *fakeDocker) update(change func(*fakeDocker)) {
	docker.mutex.Lock()
	defer docker.mutex.Unlock()
	change(docker)
}

func TestLedger(t *testing.T) {
	for _, test := range []struct {
		name    string
		change  func(*fakeDocker)
		discard string
		problem string
	}{
		{name: "a healthy node", change: func(*fakeDocker) {}, discard: "0"},
		{
			name: "a container that exited",
			change: func(docker *fakeDocker) {
				docker.state.Running, docker.state.ExitCode, docker.state.OOMKilled = false, 137, true
			},
			discard: "0",
			problem: "node: exited with code 137, out of memory",
		},
		{
			name:    "a container that restarted",
			change:  func(docker *fakeDocker) { docker.state.StartedAt = "later" },
			discard: "0",
			problem: "node: restarted",
		},
		{
			name:    "a container that Docker restarted",
			change:  func(docker *fakeDocker) { docker.restarts = 1 },
			discard: "0",
			problem: "node: restarted",
		},
		{
			name:    "a container with no state",
			change:  func(docker *fakeDocker) { docker.stateless = true },
			discard: "0",
			problem: "node: has no state",
		},
		{name: "a meter that rose", change: func(*fakeDocker) {}, discard: "3", problem: "node: txpool_pending_discard rose by 3"},
		{name: "a meter that rose a million", change: func(*fakeDocker) {}, discard: "1000000", problem: "node: txpool_pending_discard rose by 1000000"},
		{
			name:    "a crash in the log",
			change:  func(docker *fakeDocker) { docker.logs = "INFO [11-16] Imported\npanic: runtime error\n" },
			discard: "0",
			problem: "node: logged panic: runtime error",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			discard := "0"
			var mutex sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				mutex.Lock()
				defer mutex.Unlock()
				_, _ = writer.Write([]byte("# TYPE txpool_pending_discard gauge\ntxpool_pending_discard " + discard + "\n"))
			}))
			defer server.Close()
			docker := &fakeDocker{
				state:    containertypes.State{Running: true, StartedAt: "earlier"},
				hostPort: server.URL[strings.LastIndex(server.URL, ":")+1:],
			}

			metrics, err := metricsURL(t.Context(), docker, "node", 6060)
			require.NoError(t, err)
			ledger := NewLedger(docker)
			require.NoError(t, ledger.Watch(t.Context(), Container{Name: "node", MetricsURL: metrics}))
			docker.update(test.change)
			mutex.Lock()
			discard = test.discard
			mutex.Unlock()

			problems, err := ledger.Problems(t.Context())
			require.NoError(t, err)
			if test.problem == "" {
				require.Empty(t, problems)
				return
			}
			require.Equal(t, []string{test.problem}, problems)
		})
	}
}

func TestLedgerLogs(t *testing.T) {
	docker := &fakeDocker{state: containertypes.State{Running: true}}
	ledger := NewLedger(docker)
	before := time.Now()
	require.NoError(t, ledger.Watch(t.Context(), Container{Name: "node"}))
	docker.update(func(docker *fakeDocker) { docker.logs = strings.Repeat("panic: runtime error\n", 5) })

	problems, err := ledger.Problems(t.Context())
	require.NoError(t, err)
	require.Len(t, problems, 3, "at most three lines of each signature")

	seconds, err := strconv.ParseFloat(docker.since, 64)
	require.NoError(t, err)
	require.InDelta(t, float64(before.UnixNano())/1e9, seconds, 1, "the logs since Watch")
	require.Regexp(t, `^[0-9]+\.[0-9]{9}$`, docker.since, "to the nanosecond")
}

func TestReadMeters(t *testing.T) {
	page := "# TYPE chain_head gauge\nchain_head 7\nchain_inserts {quantile=\"0.5\"} 9\nnot_a_number x\n"
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte(page))
	}))
	defer server.Close()

	meters, err := readMeters(t.Context(), server.URL)
	require.NoError(t, err)
	require.Equal(t, map[string]float64{"chain_head": 7}, meters, "comments, labelled and bad values are skipped")

	page = "# nothing\n"
	_, err = readMeters(t.Context(), server.URL)
	require.ErrorContains(t, err, "has no values")

	status = http.StatusServiceUnavailable
	_, err = readMeters(t.Context(), server.URL)
	require.ErrorContains(t, err, "503")
}

func TestParticipantContainers(t *testing.T) {
	docker := &fakeDocker{port: participantMetricsPort, hostPort: "32000"}
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
	require.ErrorContains(t, err, "port 9001 of el-1--a is not published")
}
