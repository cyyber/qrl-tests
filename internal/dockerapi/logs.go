package dockerapi

import (
	"bytes"
	"context"

	"github.com/moby/moby/api/pkg/stdcopy"
	dockerclient "github.com/moby/moby/client"
)

// LogReader reads a container's logs, as the Docker client does.
type LogReader interface {
	ContainerLogs(context.Context, string, dockerclient.ContainerLogsOptions) (dockerclient.ContainerLogsResult, error)
}

// Logs returns a container's stdout and stderr, from the window that options
// selects.
func Logs(ctx context.Context, docker LogReader, container string, options dockerclient.ContainerLogsOptions) (string, error) {
	options.ShowStdout, options.ShowStderr = true, true
	logs, err := docker.ContainerLogs(ctx, container, options)
	if err != nil {
		return "", err
	}
	defer logs.Close()

	var output bytes.Buffer
	if _, err := stdcopy.StdCopy(&output, &output, logs); err != nil {
		return "", err
	}
	return output.String(), nil
}
