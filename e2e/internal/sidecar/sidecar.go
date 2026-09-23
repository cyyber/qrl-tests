// Package sidecar runs helper containers next to the Kurtosis devnet.
// Sidecars reach the devnet through its host-published ports. They are not
// Kurtosis services, so the lane diagnostics don't collect their logs; errors
// include the end of a sidecar's output instead.
package sidecar

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/cyyber/qrl-tests/internal/containerfiles"
	"github.com/cyyber/qrl-tests/internal/dockerapi"
	"github.com/moby/moby/api/pkg/stdcopy"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	dockerclient "github.com/moby/moby/client"
)

const (
	cleanupTimeout = 30 * time.Second
	logTimeout     = 10 * time.Second
	exitLogTail    = "50"
	// labelKey tags every sidecar container, so containers left behind by an
	// interrupted test run can be found with a label filter.
	labelKey = "qrl-tests.sidecar"
)

// Client is the part of the Docker client that sidecars use.
type Client interface {
	ContainerAttach(context.Context, string, dockerclient.ContainerAttachOptions) (dockerclient.ContainerAttachResult, error)
	ContainerCreate(context.Context, dockerclient.ContainerCreateOptions) (dockerclient.ContainerCreateResult, error)
	ContainerInspect(context.Context, string, dockerclient.ContainerInspectOptions) (dockerclient.ContainerInspectResult, error)
	ContainerLogs(context.Context, string, dockerclient.ContainerLogsOptions) (dockerclient.ContainerLogsResult, error)
	ContainerRemove(context.Context, string, dockerclient.ContainerRemoveOptions) (dockerclient.ContainerRemoveResult, error)
	ContainerStart(context.Context, string, dockerclient.ContainerStartOptions) (dockerclient.ContainerStartResult, error)
	ContainerWait(context.Context, string, dockerclient.ContainerWaitOptions) dockerclient.ContainerWaitResult
	CopyFromContainer(context.Context, string, dockerclient.CopyFromContainerOptions) (dockerclient.CopyFromContainerResult, error)
	CopyToContainer(context.Context, string, dockerclient.CopyToContainerOptions) (dockerclient.CopyToContainerResult, error)
	ExecCreate(context.Context, string, dockerclient.ExecCreateOptions) (dockerclient.ExecCreateResult, error)
	ExecAttach(context.Context, string, dockerclient.ExecAttachOptions) (dockerclient.ExecAttachResult, error)
	ExecInspect(context.Context, string, dockerclient.ExecInspectOptions) (dockerclient.ExecInspectResult, error)
}

// File is a file a sidecar copies into its container or reads out of it.
type File = containerfiles.File

// FilesIn places files in dir under their base names.
func FilesIn(dir string, files []File) ([]File, error) {
	placed := make([]File, len(files))
	for index, file := range files {
		name := path.Base(strings.TrimSpace(file.Name))
		if name == "." || name == "/" {
			return nil, errors.New("file name is empty")
		}
		file.Name = path.Join(dir, name)
		placed[index] = file
	}
	return placed, nil
}

// Spec describes a sidecar container.
type Spec struct {
	// Name identifies the sidecar in errors, such as "validator sidecar".
	Name       string
	Image      string
	Entrypoint []string
	Cmd        []string
	Env        []string
	Files      []File
	// Stdin keeps the container's standard input open for Attach.
	Stdin bool
	// Port, when set, is published on 127.0.0.1 at a host port Docker picks.
	Port uint16
}

// Container is a sidecar container. Close removes it.
type Container struct {
	client Client
	id     string
	name   string
	port   network.Port
}

// Start creates and starts the container, removing it again on any failure.
func Start(ctx context.Context, client Client, spec Spec) (*Container, error) {
	container, err := create(ctx, client, spec, false)
	if err != nil {
		return nil, err
	}
	if err := container.start(ctx); err != nil {
		return nil, container.abort(err)
	}
	return container, nil
}

// Run starts the container and waits for it to exit. A non-zero exit is an
// *ExitError and removes the container; on success the container stays so its
// output can be read, and the caller must Close it.
func Run(ctx context.Context, client Client, spec Spec) (*Container, error) {
	container, err := create(ctx, client, spec, false)
	if err != nil {
		return nil, err
	}
	waiter, err := container.startWatched(ctx)
	if err != nil {
		return nil, container.abort(err)
	}
	if err := container.waitForExit(ctx, waiter); err != nil {
		return nil, container.abort(container.WithLogs(err))
	}
	return container, nil
}

// waitForExit reports a non-zero exit as an *ExitError.
func (container *Container) waitForExit(ctx context.Context, waiter dockerclient.ContainerWaitResult) error {
	select {
	case status := <-waiter.Result:
		if status.Error != nil {
			return fmt.Errorf("wait for %s: %s", container.name, status.Error.Message)
		}
		if status.StatusCode != 0 {
			return &ExitError{name: container.name, status: fmt.Sprintf("exited with code %d", status.StatusCode)}
		}
		return nil
	case err := <-waiter.Error:
		// The waiter reads with ctx, so it fails as well once ctx ends; report
		// that as the cancellation it is.
		if ctx.Err() == nil {
			return fmt.Errorf("wait for %s: %w", container.name, err)
		}
	case <-ctx.Done():
	}
	return fmt.Errorf("wait for %s: %w", container.name, context.Cause(ctx))
}

// AttachDocker is Attach with its own Docker client, closed by Close.
func AttachDocker(ctx context.Context, spec Spec) (*Process, error) {
	client, err := dockerapi.New()
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	process, err := Attach(ctx, client, spec)
	if err != nil {
		return nil, errors.Join(err, client.Close())
	}
	process.closeClient = client.Close
	return process, nil
}

// Attach starts the container attached to its output, and to its stdin if
// spec.Stdin is set. On failure, the container is removed.
func Attach(ctx context.Context, client Client, spec Spec) (*Process, error) {
	container, err := create(ctx, client, spec, true)
	if err != nil {
		return nil, err
	}
	processCtx, cancel := context.WithCancel(ctx)
	attached, err := client.ContainerAttach(processCtx, container.id, dockerclient.ContainerAttachOptions{
		Stream: true,
		Stdin:  spec.Stdin,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		cancel()
		return nil, container.abort(fmt.Errorf("attach to %s container: %w", spec.Name, err))
	}
	process := &Process{Container: container, ctx: processCtx, cancel: cancel, attach: attached}
	if process.waiter, err = container.startWatched(processCtx); err != nil {
		return nil, errors.Join(err, process.Close())
	}
	return process, nil
}

// startWatched registers an exit waiter, then starts the container, so a fast
// exit is not missed.
func (container *Container) startWatched(ctx context.Context) (dockerclient.ContainerWaitResult, error) {
	waiter := container.client.ContainerWait(ctx, container.id, dockerclient.ContainerWaitOptions{
		Condition: containertypes.WaitConditionNextExit,
	})
	select {
	case err := <-waiter.Error:
		if err != nil {
			if ctx.Err() != nil {
				err = context.Cause(ctx)
			}
			return waiter, fmt.Errorf("register %s exit waiter: %w", container.name, err)
		}
	default:
	}
	return waiter, container.start(ctx)
}

func (container *Container) start(ctx context.Context) error {
	if _, err := container.client.ContainerStart(ctx, container.id, dockerclient.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("start %s container: %w", container.name, err)
	}
	return nil
}

func create(ctx context.Context, client Client, spec Spec, attached bool) (*Container, error) {
	archive, err := containerfiles.Archive(spec.Files)
	if err != nil {
		return nil, fmt.Errorf("archive %s files: %w", spec.Name, err)
	}

	config := &containertypes.Config{
		Image:        spec.Image,
		Entrypoint:   spec.Entrypoint,
		Cmd:          spec.Cmd,
		Env:          spec.Env,
		Labels:       map[string]string{labelKey: spec.Name},
		AttachStdin:  spec.Stdin,
		AttachStdout: attached,
		AttachStderr: attached,
		OpenStdin:    spec.Stdin,
		StdinOnce:    spec.Stdin,
	}
	hostConfig := &containertypes.HostConfig{ExtraHosts: []string{containerHost + ":host-gateway"}}
	var port network.Port
	if spec.Port != 0 {
		var ok bool
		port, ok = network.PortFrom(spec.Port, network.TCP)
		if !ok {
			return nil, fmt.Errorf("%s port %d is invalid", spec.Name, spec.Port)
		}
		config.ExposedPorts = network.PortSet{port: {}}
		hostConfig.PortBindings = network.PortMap{port: {{HostIP: netip.MustParseAddr("127.0.0.1")}}}
	}

	created, err := client.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{Config: config, HostConfig: hostConfig})
	if err != nil {
		return nil, fmt.Errorf("create %s container: %w", spec.Name, err)
	}
	if created.ID == "" {
		return nil, fmt.Errorf("create %s container: Docker returned no container ID", spec.Name)
	}
	container := &Container{client: client, id: created.ID, name: spec.Name, port: port}

	if _, err := client.CopyToContainer(ctx, created.ID, dockerclient.CopyToContainerOptions{
		DestinationPath: "/",
		Content:         bytes.NewReader(archive),
	}); err != nil {
		return nil, container.abort(fmt.Errorf("copy %s files: %w", spec.Name, err))
	}
	return container, nil
}

func (container *Container) abort(err error) error {
	return errors.Join(err, container.Close())
}

// Close removes the container on its own deadline, since the caller's context
// may be the reason it is being torn down.
func (container *Container) Close() error {
	if container == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	if _, err := container.client.ContainerRemove(ctx, container.id, dockerclient.ContainerRemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("remove %s container: %w", container.name, err)
	}
	return nil
}

// PublishedPort returns the host port bound to the Spec's Port. It returns an
// *ExitError once the container has stopped.
func (container *Container) PublishedPort(ctx context.Context) (string, error) {
	if !container.port.IsValid() {
		return "", fmt.Errorf("%s publishes no port", container.name)
	}
	inspected, err := container.client.ContainerInspect(ctx, container.id, dockerclient.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("inspect %s container: %w", container.name, err)
	}
	if state := inspected.Container.State; state != nil && !state.Running {
		status := "is " + string(state.Status)
		if state.Status == containertypes.StateExited {
			status = fmt.Sprintf("exited with code %d", state.ExitCode)
		}
		if state.Error != "" {
			status += ": " + state.Error
		}
		return "", container.WithLogs(&ExitError{name: container.name, status: status})
	}
	return publishedHostPort(inspected.Container, container.port)
}

func publishedHostPort(inspected containertypes.InspectResponse, port network.Port) (string, error) {
	if inspected.NetworkSettings == nil {
		return "", errors.New("container has no network settings")
	}
	bindings := inspected.NetworkSettings.Ports[port]
	if len(bindings) == 0 || bindings[0].HostPort == "" {
		return "", fmt.Errorf("container port %s is not published", port)
	}
	return bindings[0].HostPort, nil
}

// ReadFile copies one file out of the container.
func (container *Container) ReadFile(ctx context.Context, source string) ([]byte, error) {
	return containerfiles.ReadFile(ctx, container.client, container.id, source)
}

// ReadDir copies the regular files under dir out of the container, named by
// their paths inside it.
func (container *Container) ReadDir(ctx context.Context, dir string) ([]File, error) {
	return containerfiles.Read(ctx, container.client, container.id, dir)
}

// WithLogs appends the end of the container's output to err, for failures the
// sidecar's own logs explain.
func (container *Container) WithLogs(err error) error {
	logs, logsErr := container.logs()
	switch {
	case logsErr != nil:
		return fmt.Errorf("%w\n(logs unavailable: %v)", err, logsErr)
	case logs == "":
		return err
	default:
		return fmt.Errorf("%w\nlast log lines:\n%s", err, logs)
	}
}

// logs reads the end of the container's output on its own deadline, since
// logs matter most once the caller's context has run out.
func (container *Container) logs() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	logs, err := container.client.ContainerLogs(ctx, container.id, dockerclient.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       exitLogTail,
	})
	if err != nil {
		return "", err
	}
	defer logs.Close()

	var output bytes.Buffer
	if _, err := stdcopy.StdCopy(&output, &output, logs); err != nil {
		return "", err
	}
	return strings.TrimSpace(output.String()), nil
}

// Exec runs command inside the container and returns its combined output. A
// non-zero exit code is an error that carries the output.
func (container *Container) Exec(ctx context.Context, command ...string) (string, error) {
	commandLine := strings.Join(command, " ")
	created, err := container.client.ExecCreate(ctx, container.id, dockerclient.ExecCreateOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          command,
	})
	if err != nil {
		return "", fmt.Errorf("create exec %s: %w", commandLine, err)
	}
	attached, err := container.client.ExecAttach(ctx, created.ID, dockerclient.ExecAttachOptions{})
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
	inspected, err := container.client.ExecInspect(ctx, created.ID, dockerclient.ExecInspectOptions{})
	if err != nil {
		return output.String(), fmt.Errorf("inspect exec %s: %w", commandLine, err)
	}
	if inspected.ExitCode != 0 {
		return output.String(), fmt.Errorf("%s: exit %d: %s", commandLine, inspected.ExitCode, strings.TrimSpace(output.String()))
	}
	return output.String(), nil
}

// Process is a sidecar container started by Attach, with its standard streams
// attached. Close detaches and removes it.
type Process struct {
	*Container
	ctx        context.Context
	cancel     context.CancelFunc
	attach     dockerclient.ContainerAttachResult
	waiter     dockerclient.ContainerWaitResult
	detachOnce sync.Once
	// closeClient is set by AttachDocker.
	closeClient func() error
}

// Output copies the container's stdout and stderr to destination until the
// streams close.
func (process *Process) Output(destination io.Writer) error {
	_, err := stdcopy.StdCopy(destination, destination, process.attach.Reader)
	return err
}

// CloseInput writes final to the container's stdin and closes it. If ctx ends
// first, the process is detached so the blocked write is released.
func (process *Process) CloseInput(ctx context.Context, final string) error {
	done := make(chan error, 1)
	go func() {
		_, writeErr := io.WriteString(process.attach.Conn, final)
		if writeErr != nil {
			writeErr = fmt.Errorf("write %s input: %w", process.name, writeErr)
		}
		closeErr := process.attach.CloseWrite()
		if closeErr != nil {
			closeErr = fmt.Errorf("close %s input: %w", process.name, closeErr)
		}
		done <- errors.Join(writeErr, closeErr)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		process.Detach()
		return context.Cause(ctx)
	}
}

// Wait blocks until the container exits. A non-zero exit is an *ExitError.
func (process *Process) Wait() error {
	return process.waitForExit(process.ctx, process.waiter)
}

// Detach releases the attached streams and the exit waiter, leaving the
// container in place.
func (process *Process) Detach() {
	process.detachOnce.Do(func() {
		process.cancel()
		process.attach.Close()
	})
}

// Close detaches the process and removes its container.
func (process *Process) Close() error {
	if process == nil {
		return nil
	}
	process.Detach()
	err := process.Container.Close()
	if process.closeClient != nil {
		err = errors.Join(err, process.closeClient())
	}
	return err
}

// ExitError reports a sidecar container that stopped.
type ExitError struct {
	name   string
	status string
}

func (err *ExitError) Error() string {
	return err.name + " container " + err.status
}
