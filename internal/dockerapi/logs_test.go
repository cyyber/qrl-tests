package dockerapi

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"testing"

	"github.com/moby/moby/api/pkg/stdcopy"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

type fakeLogs struct {
	stdout, stderr string
	options        dockerclient.ContainerLogsOptions
}

func (docker *fakeLogs) ContainerLogs(_ context.Context, _ string, options dockerclient.ContainerLogsOptions) (dockerclient.ContainerLogsResult, error) {
	docker.options = options
	var stream bytes.Buffer
	for _, frame := range []struct {
		kind stdcopy.StdType
		text string
	}{{stdcopy.Stdout, docker.stdout}, {stdcopy.Stderr, docker.stderr}} {
		header := make([]byte, 8)
		header[0] = byte(frame.kind)
		binary.BigEndian.PutUint32(header[4:], uint32(len(frame.text)))
		stream.Write(append(header, frame.text...))
	}
	return io.NopCloser(&stream), nil
}

func TestLogs(t *testing.T) {
	docker := &fakeLogs{stdout: "imported\n", stderr: "panic: boom\n"}

	logs, err := Logs(t.Context(), docker, "node", dockerclient.ContainerLogsOptions{Tail: "10"})
	require.NoError(t, err)
	require.Equal(t, "imported\npanic: boom\n", logs)
	require.Equal(t, dockerclient.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Tail: "10"}, docker.options)
}
