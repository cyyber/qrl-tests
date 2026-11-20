package health

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"errors"
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

// fakeDocker is one container.
type fakeDocker struct {
	state    containertypes.State
	err      error
	port     uint16
	hostPort string
	logs     string
	since    string
}

func (docker *fakeDocker) ContainerInspect(context.Context, string, dockerclient.ContainerInspectOptions) (dockerclient.ContainerInspectResult, error) {
	if docker.err != nil {
		return dockerclient.ContainerInspectResult{}, docker.err
	}
	port, _ := network.PortFrom(docker.port, network.TCP)
	return dockerclient.ContainerInspectResult{Container: containertypes.InspectResponse{
		State: new(docker.state),
		NetworkSettings: &containertypes.NetworkSettings{
			Ports: network.PortMap{port: {{HostIP: netip.IPv4Unspecified(), HostPort: docker.hostPort}}},
		},
	}}, nil
}

func (docker *fakeDocker) ContainerLogs(_ context.Context, _ string, options dockerclient.ContainerLogsOptions) (dockerclient.ContainerLogsResult, error) {
	docker.since = options.Since
	// Docker frames each write with its stream and length.
	frame := make([]byte, 8, 8+len(docker.logs))
	frame[0] = 1
	binary.BigEndian.PutUint32(frame[4:], uint32(len(docker.logs)))
	return io.NopCloser(bytes.NewReader(append(frame, docker.logs...))), nil
}

func TestStateProblems(t *testing.T) {
	for _, test := range []struct {
		name      string
		state     *containertypes.State
		problems  []string
		restarted bool
	}{
		{name: "running", state: &containertypes.State{Running: true, StartedAt: "earlier"}},
		{name: "no state", problems: []string{"has no state"}},
		{
			name:     "exited",
			state:    &containertypes.State{ExitCode: 137, OOMKilled: true},
			problems: []string{"exited with code 137, out of memory"},
		},
		{
			name:      "restarted",
			state:     &containertypes.State{Running: true, StartedAt: "later"},
			problems:  []string{"restarted"},
			restarted: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			problems, restarted := stateProblems(test.state, "earlier")
			require.Equal(t, test.problems, problems)
			require.Equal(t, test.restarted, restarted)
		})
	}
}

func TestContainerStates(t *testing.T) {
	for _, test := range []struct {
		name string
		// meter is the trouble meter served, txpool_pending_discard when empty;
		// before and after are its values at record and at problems, "0" when
		// empty.
		meter, before, after string
		change               func(*fakeDocker)
		problems             []string
	}{
		{name: "a healthy node"},
		{name: "a meter that rose", after: "3", problems: []string{"node: txpool_pending_discard rose by 3"}},
		{name: "a meter that rose a million", after: "1000000", problems: []string{"node: txpool_pending_discard rose by 1000000"}},
		{
			name:     "a request that expired without a reply",
			meter:    "p2p_lost_qrl_68_0x03",
			after:    "2",
			problems: []string{"node: p2p_lost_qrl_68_0x03 rose by 2"},
		},
		{
			name:     "a meter that rose after a restart",
			before:   "5",
			after:    "2",
			change:   func(docker *fakeDocker) { docker.state.StartedAt = "later" },
			problems: []string{"node: restarted", "node: txpool_pending_discard rose by 2"},
		},
		{
			name:     "a crash in the log",
			change:   func(docker *fakeDocker) { docker.logs = "INFO [11-16] Imported\npanic: runtime error\n" },
			problems: []string{"node: logged panic: runtime error"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			meter := cmp.Or(test.meter, "txpool_pending_discard")
			value := cmp.Or(test.before, "0")
			var mutex sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				mutex.Lock()
				defer mutex.Unlock()
				_, _ = writer.Write([]byte(meter + " " + value + "\n"))
			}))
			defer server.Close()
			docker := &fakeDocker{state: containertypes.State{Running: true, StartedAt: "earlier"}}
			containers := &containerStates{docker: docker}
			require.NoError(t, containers.record(t.Context(), Container{Name: "node", MetricsURL: server.URL}))

			if test.change != nil {
				test.change(docker)
			}
			mutex.Lock()
			value = cmp.Or(test.after, "0")
			mutex.Unlock()

			problems, err := containers.problems(t.Context())
			require.NoError(t, err)
			require.Equal(t, test.problems, problems)
		})
	}
}

func TestContainerStatesMetricsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("txpool_pending_discard 0\n"))
	}))
	defer server.Close()
	docker := &fakeDocker{state: containertypes.State{Running: true, StartedAt: "earlier"}}
	containers := &containerStates{docker: docker}
	require.NoError(t, containers.record(t.Context(), Container{Name: "node", MetricsURL: server.URL}))

	// A restarted node that does not serve its metrics yet.
	docker.state.StartedAt = "later"
	server.Close()

	problems, err := containers.problems(t.Context())
	require.ErrorContains(t, err, "node: ")
	require.Equal(t, []string{"node: restarted"}, problems)
}

func TestContainerStatesRetire(t *testing.T) {
	docker := &fakeDocker{state: containertypes.State{Running: true, StartedAt: "earlier"}}
	containers := &containerStates{docker: docker}
	require.NoError(t, containers.record(t.Context(), Container{Name: "node"}))
	docker.state.StartedAt = "later"

	require.NoError(t, containers.retire(t.Context(), "node"))
	// Reading it again would fail.
	docker.err = errors.New("no such container")
	problems, err := containers.problems(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"node: restarted"}, problems)

	require.ErrorContains(t, containers.retire(t.Context(), "node"), "node is not monitored")
}

func TestContainerStatesInspectError(t *testing.T) {
	docker := &fakeDocker{state: containertypes.State{Running: true}}
	containers := &containerStates{docker: docker}
	require.NoError(t, containers.record(t.Context(), Container{Name: "node"}))

	docker.err = errors.New("gone")
	require.ErrorContains(t, containers.retire(t.Context(), "node"), "node: gone")
	problems, err := containers.problems(t.Context())
	require.NoError(t, err, "a retired container is not read again")
	require.Empty(t, problems)

	require.ErrorContains(t, containers.record(t.Context(), Container{Name: "other"}), "inspect other: gone")
}

func TestContainerStatesLogs(t *testing.T) {
	docker := &fakeDocker{state: containertypes.State{Running: true}}
	containers := &containerStates{docker: docker}
	before := time.Now()
	require.NoError(t, containers.record(t.Context(), Container{Name: "node"}))
	docker.logs = strings.Repeat("panic: runtime error\n", 5) + "CRIT [11-16] Failed\n"

	problems, err := containers.problems(t.Context())
	require.NoError(t, err)
	require.Len(t, problems, 4, "at most three lines of each signature")
	require.Equal(t, "node: logged CRIT [11-16] Failed", problems[3])

	seconds, err := strconv.ParseFloat(docker.since, 64)
	require.NoError(t, err)
	require.InDelta(t, float64(before.UnixNano())/1e9, seconds, 1, "the logs since record")
	require.Regexp(t, `^[0-9]+\.[0-9]{9}$`, docker.since, "to the nanosecond")
}

func TestReadMetrics(t *testing.T) {
	serve := func(t *testing.T, status int, page string) string {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(status)
			_, _ = writer.Write([]byte(page))
		}))
		t.Cleanup(server.Close)
		return server.URL
	}

	t.Run("skips comments, labelled and bad values", func(t *testing.T) {
		page := "# TYPE chain_head gauge\nchain_head 7\nchain_inserts {quantile=\"0.5\"} 9\nnot_a_number x\n"
		values, err := readMetrics(t.Context(), serve(t, http.StatusOK, page))
		require.NoError(t, err)
		require.Equal(t, map[string]float64{"chain_head": 7}, values)
	})
	t.Run("an empty page", func(t *testing.T) {
		_, err := readMetrics(t.Context(), serve(t, http.StatusOK, "# nothing\n"))
		require.ErrorContains(t, err, "has no values")
	})
	t.Run("a failed request", func(t *testing.T) {
		_, err := readMetrics(t.Context(), serve(t, http.StatusServiceUnavailable, ""))
		require.ErrorContains(t, err, "503")
	})
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
	require.ErrorContains(t, err, "el-1--a: port 9001 is not published")
}
