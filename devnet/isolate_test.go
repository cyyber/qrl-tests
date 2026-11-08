package devnet

import (
	"context"
	"errors"
	"io"
	"iter"
	"net/netip"
	"strconv"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/jsonstream"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestIsolateService(t *testing.T) {
	client := newFakeIsolator()
	peers := []netip.Addr{netip.MustParseAddr("172.16.0.11"), netip.MustParseAddr("172.16.0.13")}

	rejoin, err := isolateService(t.Context(), client, "consensus-service", peers)
	require.NoError(t, err)
	require.Equal(t, dockerclient.Filters{
		"label": {kurtosisServiceUUIDDockerLabel + "=consensus-service": true},
	}, client.listed.Filters)
	require.Equal(t, []string{isolationImage}, client.pulled)
	require.Len(t, client.created, 1)
	helper := client.created[0]
	require.Equal(t, isolationImage, helper.Config.Image)
	require.True(t, helper.HostConfig.Privileged)
	require.Equal(t, containertypes.PidMode("host"), helper.HostConfig.PidMode)
	require.Equal(t, []string{
		"nsenter", "-t", "1", "-m", "--", "nsenter", "-t", "4242", "-n", "--", "sh", "-c",
		"set -e\n" +
			"iptables -I INPUT -s 172.16.0.11 -j DROP\niptables -I OUTPUT -d 172.16.0.11 -j DROP\n" +
			"iptables -I INPUT -s 172.16.0.13 -j DROP\niptables -I OUTPUT -d 172.16.0.13 -j DROP",
	}, helper.Config.Cmd)
	require.Equal(t, []string{"helper-1"}, client.removed)

	require.NoError(t, rejoin(t.Context()))
	require.Len(t, client.created, 2)
	rejoinCmd := client.created[1].Config.Cmd
	require.Contains(t, rejoinCmd[len(rejoinCmd)-1], "iptables -D INPUT -s 172.16.0.11 -j DROP")
	require.Equal(t, []string{"helper-1", "helper-2"}, client.removed)
	// The image is pulled only once it is missing.
	require.Len(t, client.pulled, 1)
}

func TestEnsureImage(t *testing.T) {
	client := newFakeIsolator()
	require.NoError(t, ensureImage(t.Context(), client, isolationImage))
	require.NoError(t, ensureImage(t.Context(), client, isolationImage))
	// The image is pulled only while it is missing.
	require.Equal(t, []string{isolationImage}, client.pulled)
}

func TestIsolateServiceErrors(t *testing.T) {
	peers := []netip.Addr{netip.MustParseAddr("172.16.0.11")}

	_, err := isolateService(t.Context(), newFakeIsolator(), "consensus-service", nil)
	require.EqualError(t, err, `isolate service "consensus-service": no peers`)

	client := newFakeIsolator()
	client.containers = nil
	_, err = isolateService(t.Context(), client, "consensus-service", peers)
	require.ErrorContains(t, err, `find container: expected one running Docker container for service "consensus-service", found 0`)
	require.Empty(t, client.created)

	client = newFakeIsolator()
	client.pid = 0
	_, err = isolateService(t.Context(), client, "consensus-service", peers)
	require.EqualError(t, err, `service "consensus-service" has no running process`)
	require.Empty(t, client.created)

	// A failed insert is followed by a cleanup that deletes whichever rules
	// it left, without stopping at one that is missing.
	client = newFakeIsolator()
	client.exitCodes = []int64{3}
	_, err = isolateService(t.Context(), client, "consensus-service", peers)
	require.EqualError(t, err, `isolate service "consensus-service": helper exited with code 3`)
	require.Len(t, client.created, 2)
	cleanup := client.created[1].Config.Cmd
	require.Equal(t,
		"iptables -D INPUT -s 172.16.0.11 -j DROP || true\niptables -D OUTPUT -d 172.16.0.11 -j DROP || true",
		cleanup[len(cleanup)-1],
	)
	require.Equal(t, []string{"helper-1", "helper-2"}, client.removed)

	client = newFakeIsolator()
	client.exitCodes = []int64{3, 4}
	_, err = isolateService(t.Context(), client, "consensus-service", peers)
	require.EqualError(t, err, "isolate service \"consensus-service\": helper exited with code 3\n"+
		"remove the rules of service \"consensus-service\": helper exited with code 4")

	client = newFakeIsolator()
	client.exitCodes = []int64{0, 1}
	rejoin, err := isolateService(t.Context(), client, "consensus-service", peers)
	require.NoError(t, err)
	require.EqualError(t, rejoin(t.Context()), `rejoin service "consensus-service": helper exited with code 1`)
	// A failed rejoin is also followed by the cleanup, so no rule after the
	// one it stopped at stays in place.
	require.Len(t, client.created, 3)
	cleanup = client.created[2].Config.Cmd
	require.Equal(t, cleanupRules(peers), cleanup[len(cleanup)-1])
}

type fakeIsolator struct {
	fakeDocker
	pid       int
	exitCodes []int64
	pulled    []string
	created   []dockerclient.ContainerCreateOptions
	removed   []string
}

func newFakeIsolator() *fakeIsolator {
	return &fakeIsolator{
		fakeDocker: fakeDocker{
			containers: []containertypes.Summary{{ID: "beacon-container"}},
			inspectErr: errors.New("no such image"),
		},
		pid: 4242,
	}
}

func (client *fakeIsolator) ImagePull(_ context.Context, image string, _ dockerclient.ImagePullOptions) (dockerclient.ImagePullResponse, error) {
	client.pulled = append(client.pulled, image)
	client.inspectErr = nil
	return fakePull{}, nil
}

func (client *fakeIsolator) ContainerInspect(context.Context, string, dockerclient.ContainerInspectOptions) (dockerclient.ContainerInspectResult, error) {
	return dockerclient.ContainerInspectResult{Container: containertypes.InspectResponse{
		State: &containertypes.State{Pid: client.pid},
	}}, nil
}

func (client *fakeIsolator) ContainerCreate(_ context.Context, options dockerclient.ContainerCreateOptions) (dockerclient.ContainerCreateResult, error) {
	client.created = append(client.created, options)
	return dockerclient.ContainerCreateResult{ID: "helper-" + strconv.Itoa(len(client.created))}, nil
}

func (client *fakeIsolator) ContainerStart(context.Context, string, dockerclient.ContainerStartOptions) (dockerclient.ContainerStartResult, error) {
	return dockerclient.ContainerStartResult{}, nil
}

func (client *fakeIsolator) ContainerWait(context.Context, string, dockerclient.ContainerWaitOptions) dockerclient.ContainerWaitResult {
	var code int64
	if run := len(client.created) - 1; run < len(client.exitCodes) {
		code = client.exitCodes[run]
	}
	result := make(chan containertypes.WaitResponse, 1)
	result <- containertypes.WaitResponse{StatusCode: code}
	return dockerclient.ContainerWaitResult{Result: result, Error: make(chan error)}
}

func (client *fakeIsolator) ContainerRemove(_ context.Context, containerID string, _ dockerclient.ContainerRemoveOptions) (dockerclient.ContainerRemoveResult, error) {
	client.removed = append(client.removed, containerID)
	return dockerclient.ContainerRemoveResult{}, nil
}

type fakePull struct{}

func (fakePull) Read([]byte) (int, error) { return 0, io.EOF }
func (fakePull) Close() error             { return nil }
func (fakePull) Wait(context.Context) error {
	return nil
}

func (fakePull) JSONMessages(context.Context) iter.Seq2[jsonstream.Message, error] {
	return func(func(jsonstream.Message, error) bool) {}
}
