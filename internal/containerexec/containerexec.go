// Package containerexec runs commands inside running Docker containers.
package containerexec

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/moby/moby/api/pkg/stdcopy"
	dockerclient "github.com/moby/moby/client"
)

// Client is the part of the Docker client Run uses.
type Client interface {
	ExecCreate(context.Context, string, dockerclient.ExecCreateOptions) (dockerclient.ExecCreateResult, error)
	ExecAttach(context.Context, string, dockerclient.ExecAttachOptions) (dockerclient.ExecAttachResult, error)
	ExecInspect(context.Context, string, dockerclient.ExecInspectOptions) (dockerclient.ExecInspectResult, error)
}

// Run runs command inside the container and returns its combined output. A
// non-zero exit code is an error that carries the output.
func Run(ctx context.Context, client Client, containerID string, command ...string) (string, error) {
	commandLine := strings.Join(command, " ")
	created, err := client.ExecCreate(ctx, containerID, dockerclient.ExecCreateOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          command,
	})
	if err != nil {
		return "", fmt.Errorf("create exec %s: %w", commandLine, err)
	}
	attached, err := client.ExecAttach(ctx, created.ID, dockerclient.ExecAttachOptions{})
	if err != nil {
		return "", fmt.Errorf("attach exec %s: %w", commandLine, err)
	}
	defer attached.Close()
	// The attached stream does not end with ctx; close it so a command that
	// never exits cannot block past the caller's deadline.
	stop := context.AfterFunc(ctx, attached.Close)
	defer stop()

	var output bytes.Buffer
	if _, err := stdcopy.StdCopy(&output, &output, attached.Reader); err != nil {
		if ctx.Err() != nil {
			return output.String(), fmt.Errorf("exec %s: %w", commandLine, context.Cause(ctx))
		}
		return output.String(), fmt.Errorf("read exec %s: %w", commandLine, err)
	}
	inspected, err := client.ExecInspect(ctx, created.ID, dockerclient.ExecInspectOptions{})
	if err != nil {
		return output.String(), fmt.Errorf("inspect exec %s: %w", commandLine, err)
	}
	if inspected.ExitCode != 0 {
		return output.String(), fmt.Errorf("%s: exit %d: %s", commandLine, inspected.ExitCode, strings.TrimSpace(output.String()))
	}
	return output.String(), nil
}
