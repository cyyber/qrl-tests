// Package sidecartest provides an in-memory Docker client for testing sidecars.
package sidecartest

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"path"
	"slices"
	"strings"

	"github.com/moby/moby/api/pkg/stdcopy"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	dockerclient "github.com/moby/moby/client"
)

// ContainerID is the ID the fake gives every container it creates.
const ContainerID = "sidecar"

// Docker serves one sidecar container whose state, output and failures the
// test sets, and records what the code under test asked Docker to do. It is not
// safe for concurrent use.
type Docker struct {
	State    *containertypes.State
	HostPort string
	Logs     string
	// Files are served by CopyFromContainer, keyed by path, for any container.
	Files map[string][]byte

	ExitCode    int64
	WaitMessage string
	// NeverExits keeps ContainerWait from reporting an exit; like Docker's
	// client, the waiter then fails once its context ends.
	NeverExits bool

	ExecOutput   string
	ExecExitCode int
	// ExecNeverExits keeps an exec's output open until the caller closes it.
	ExecNeverExits bool

	StartErr      error
	RemoveErr     error
	LogsErr       error
	ExecCreateErr error
	ExecAttachErr error

	// Recorded calls.
	Created dockerclient.ContainerCreateOptions
	Archive []byte
	Execs   [][]string
	Removed []string
}

// NewDocker returns a fake whose sidecar is running.
func NewDocker() *Docker {
	return &Docker{
		Files: map[string][]byte{},
		State: &containertypes.State{Status: containertypes.StateRunning, Running: true},
	}
}

// ArchiveNames lists the entries of the archive copied into the container.
func (docker *Docker) ArchiveNames() ([]string, error) {
	reader := tar.NewReader(bytes.NewReader(docker.Archive))
	var names []string
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return names, nil
		}
		if err != nil {
			return nil, err
		}
		names = append(names, header.Name)
	}
}

func (docker *Docker) ContainerCreate(_ context.Context, options dockerclient.ContainerCreateOptions) (dockerclient.ContainerCreateResult, error) {
	docker.Created = options
	return dockerclient.ContainerCreateResult{ID: ContainerID}, nil
}

func (docker *Docker) ContainerInspect(context.Context, string, dockerclient.ContainerInspectOptions) (dockerclient.ContainerInspectResult, error) {
	ports := network.PortMap{}
	if docker.Created.Config != nil {
		for port := range docker.Created.Config.ExposedPorts {
			ports[port] = []network.PortBinding{{HostPort: docker.HostPort}}
		}
	}
	return dockerclient.ContainerInspectResult{Container: containertypes.InspectResponse{
		State:           docker.State,
		NetworkSettings: &containertypes.NetworkSettings{Ports: ports},
	}}, nil
}

func (docker *Docker) ContainerLogs(context.Context, string, dockerclient.ContainerLogsOptions) (dockerclient.ContainerLogsResult, error) {
	if docker.LogsErr != nil {
		return nil, docker.LogsErr
	}
	return io.NopCloser(bytes.NewReader(multiplexed(stdcopy.Stderr, docker.Logs))), nil
}

func (docker *Docker) ContainerRemove(_ context.Context, containerID string, _ dockerclient.ContainerRemoveOptions) (dockerclient.ContainerRemoveResult, error) {
	docker.Removed = append(docker.Removed, containerID)
	return dockerclient.ContainerRemoveResult{}, docker.RemoveErr
}

func (docker *Docker) ContainerStart(context.Context, string, dockerclient.ContainerStartOptions) (dockerclient.ContainerStartResult, error) {
	return dockerclient.ContainerStartResult{}, docker.StartErr
}

// CopyFromContainer serves a file, or every file under a directory, from Files
// and names the archive entries relative to the source's parent, as Docker does.
func (docker *Docker) CopyFromContainer(_ context.Context, _ string, options dockerclient.CopyFromContainerOptions) (dockerclient.CopyFromContainerResult, error) {
	source := path.Clean(options.SourcePath)
	var names []string
	if _, ok := docker.Files[source]; ok {
		names = []string{source}
	} else {
		for name := range docker.Files {
			if strings.HasPrefix(name, source+"/") {
				names = append(names, name)
			}
		}
		slices.Sort(names)
	}
	if len(names) == 0 {
		return dockerclient.CopyFromContainerResult{}, errors.New("no such file: " + options.SourcePath)
	}

	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, name := range names {
		body := docker.Files[name]
		relative := strings.TrimPrefix(name, strings.TrimSuffix(path.Dir(source), "/")+"/")
		if err := writer.WriteHeader(&tar.Header{
			Name: relative, Mode: 0o600, Typeflag: tar.TypeReg, Size: int64(len(body)),
		}); err != nil {
			return dockerclient.CopyFromContainerResult{}, err
		}
		if _, err := writer.Write(body); err != nil {
			return dockerclient.CopyFromContainerResult{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return dockerclient.CopyFromContainerResult{}, err
	}
	return dockerclient.CopyFromContainerResult{Content: io.NopCloser(&archive)}, nil
}

func (docker *Docker) ContainerWait(ctx context.Context, _ string, _ dockerclient.ContainerWaitOptions) dockerclient.ContainerWaitResult {
	result := make(chan containertypes.WaitResponse, 1)
	errs := make(chan error, 1)
	switch {
	case docker.NeverExits && ctx.Err() != nil:
		// Docker's client fails the wait request itself on a done context.
		errs <- ctx.Err()
	case docker.NeverExits:
		go func() {
			<-ctx.Done()
			errs <- ctx.Err()
		}()
	default:
		response := containertypes.WaitResponse{StatusCode: docker.ExitCode}
		if docker.WaitMessage != "" {
			response.Error = &containertypes.WaitExitError{Message: docker.WaitMessage}
		}
		result <- response
	}
	return dockerclient.ContainerWaitResult{Result: result, Error: errs}
}

func (docker *Docker) CopyToContainer(_ context.Context, _ string, options dockerclient.CopyToContainerOptions) (dockerclient.CopyToContainerResult, error) {
	archive, err := io.ReadAll(options.Content)
	docker.Archive = archive
	return dockerclient.CopyToContainerResult{}, err
}

func (docker *Docker) ExecCreate(_ context.Context, _ string, options dockerclient.ExecCreateOptions) (dockerclient.ExecCreateResult, error) {
	docker.Execs = append(docker.Execs, options.Cmd)
	return dockerclient.ExecCreateResult{ID: "exec"}, docker.ExecCreateErr
}

func (docker *Docker) ExecAttach(context.Context, string, dockerclient.ExecAttachOptions) (dockerclient.ExecAttachResult, error) {
	if docker.ExecAttachErr != nil {
		return dockerclient.ExecAttachResult{}, docker.ExecAttachErr
	}
	conn, _ := net.Pipe()
	reader := bufio.NewReader(bytes.NewReader(multiplexed(stdcopy.Stdout, docker.ExecOutput)))
	if docker.ExecNeverExits {
		// Nothing writes the other end, so reads block until conn is closed.
		reader = bufio.NewReader(conn)
	}
	return dockerclient.ExecAttachResult{HijackedResponse: dockerclient.HijackedResponse{Conn: conn, Reader: reader}}, nil
}

func (docker *Docker) ExecInspect(context.Context, string, dockerclient.ExecInspectOptions) (dockerclient.ExecInspectResult, error) {
	return dockerclient.ExecInspectResult{ExitCode: docker.ExecExitCode}, nil
}

// multiplexed frames text the way Docker streams output from a container
// without a TTY.
func multiplexed(stream stdcopy.StdType, text string) []byte {
	frame := make([]byte, 8, 8+len(text))
	frame[0] = byte(stream)
	binary.BigEndian.PutUint32(frame[4:], uint32(len(text)))
	return append(frame, text...)
}
