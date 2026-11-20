package health

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

type fakeDocker struct {
	state    containertypes.State
	err      error
	hostPort string
	logs     string
	since    string
}

func (docker *fakeDocker) ContainerInspect(context.Context, string, dockerclient.ContainerInspectOptions) (dockerclient.ContainerInspectResult, error) {
	if docker.err != nil {
		return dockerclient.ContainerInspectResult{}, docker.err
	}
	port, _ := network.PortFrom(participantMetricsPort, network.TCP)
	return dockerclient.ContainerInspectResult{Container: containertypes.InspectResponse{
		State: new(docker.state),
		NetworkSettings: &containertypes.NetworkSettings{
			Ports: network.PortMap{port: {{HostPort: docker.hostPort}}},
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
		// meter defaults to txpool_pending_discard, before and after to "0".
		meter, before, after string
		change               func(*fakeDocker)
		problems             []string
	}{
		{name: "a healthy node"},
		{name: "a meter that rose", after: "3", problems: []string{"node: txpool_pending_discard rose by 3"}},
		{name: "a large rise, written without an exponent", after: "1000000", problems: []string{"node: txpool_pending_discard rose by 1000000"}},
		{name: "a meter that fell", before: "5", after: "2"},
		{
			name:     "an exited node, its meters unread",
			after:    "3",
			change:   func(docker *fakeDocker) { docker.state.Running = false },
			problems: []string{"node: exited with code 0"},
		},
		{
			name:     "a prefixed meter that rose",
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
			var value atomic.Pointer[string]
			value.Store(new(cmp.Or(test.before, "0")))

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(meter + " " + *value.Load() + "\n"))
			}))
			defer server.Close()

			docker := &fakeDocker{state: containertypes.State{Running: true, StartedAt: "earlier"}}
			containers := &containerStates{docker: docker}
			require.NoError(t, containers.record(t.Context(), Container{Name: "node", MetricsURL: server.URL}))

			if test.change != nil {
				test.change(docker)
			}
			value.Store(new(cmp.Or(test.after, "0")))

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
	require.NoError(t, containers.record(t.Context(), Container{Name: "node"}, Container{Name: "other"}))
	docker.state.StartedAt = "later"

	require.NoError(t, containers.retire(t.Context(), "other"))
	problems, err := containers.problems(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"other: restarted", "node: restarted"}, problems, "retired problems first")

	require.ErrorContains(t, containers.retire(t.Context(), "other"), "other is not monitored")
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
	require.NoError(t, containers.record(t.Context(), Container{Name: "node"}))
	docker.logs = strings.Repeat("panic: runtime error\n", 5) + "CRIT [11-16] Failed\n"

	problems, err := containers.problems(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{
		"node: logged panic: runtime error",
		"node: logged panic: runtime error",
		"node: logged panic: runtime error",
		"node: logged CRIT [11-16] Failed",
	}, problems, "at most three lines of each signature")

	since := containers.recorded[0].since
	require.Equal(t, fmt.Sprintf("%d.%09d", since.Unix(), since.Nanosecond()), docker.since, "the logs since record")
}
