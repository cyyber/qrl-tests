package health

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

type fakeDocker struct {
	mutex    sync.Mutex
	state    containertypes.State
	restarts int
	hostPort string
	logs     string
}

func (docker *fakeDocker) ContainerInspect(context.Context, string, dockerclient.ContainerInspectOptions) (dockerclient.ContainerInspectResult, error) {
	docker.mutex.Lock()
	defer docker.mutex.Unlock()
	state := docker.state
	port, _ := network.PortFrom(6060, network.TCP)
	return dockerclient.ContainerInspectResult{Container: containertypes.InspectResponse{
		State:        &state,
		RestartCount: docker.restarts,
		NetworkSettings: &containertypes.NetworkSettings{
			Ports: network.PortMap{port: {{HostIP: netip.IPv4Unspecified(), HostPort: docker.hostPort}}},
		},
	}}, nil
}

func (docker *fakeDocker) ContainerLogs(context.Context, string, dockerclient.ContainerLogsOptions) (dockerclient.ContainerLogsResult, error) {
	docker.mutex.Lock()
	defer docker.mutex.Unlock()
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
			change:  func(docker *fakeDocker) { docker.state.StartedAt, docker.restarts = "later", 1 },
			discard: "0",
			problem: "node: restarted",
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
