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
	"net/netip"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/internal/containerfiles"
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

type Client interface {
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

type Spec struct {
	// Name identifies the sidecar in errors, such as "validator sidecar".
	Name       string
	Image      string
	Entrypoint []string
	Env        []string
	Files      []File
	// Port, when set, is published on 127.0.0.1 at a host port Docker picks.
	Port uint16
}

// Container is a sidecar container this process created and must Close.
type Container struct {
	client Client
	id     string
	name   string
	port   network.Port
}

// Start creates and starts the container, removing it again on any failure.
func Start(ctx context.Context, client Client, spec Spec) (*Container, error) {
	container, err := create(ctx, client, spec)
	if err != nil {
		return nil, err
	}
	if _, err := client.ContainerStart(ctx, container.id, dockerclient.ContainerStartOptions{}); err != nil {
		return nil, container.abort(fmt.Errorf("start %s container: %w", spec.Name, err))
	}
	return container, nil
}

// Run starts the container and waits for it to exit. A non-zero exit is an
// *ExitError and removes the container; on success the container stays so its
// output can be read, and the caller must Close it.
func Run(ctx context.Context, client Client, spec Spec) (*Container, error) {
	container, err := create(ctx, client, spec)
	if err != nil {
		return nil, err
	}
	// Register the waiter before starting, so a fast exit is not missed.
	waiter := client.ContainerWait(ctx, container.id, dockerclient.ContainerWaitOptions{
		Condition: containertypes.WaitConditionNextExit,
	})
	if _, err := client.ContainerStart(ctx, container.id, dockerclient.ContainerStartOptions{}); err != nil {
		return nil, container.abort(fmt.Errorf("start %s container: %w", spec.Name, err))
	}

	select {
	case status := <-waiter.Result:
		if status.Error != nil {
			return nil, container.abort(fmt.Errorf("wait for %s: %s", spec.Name, status.Error.Message))
		}
		if status.StatusCode != 0 {
			return nil, container.abort(container.WithLogs(&ExitError{
				name:   spec.Name,
				status: fmt.Sprintf("exited with code %d", status.StatusCode),
			}))
		}
		return container, nil
	case err := <-waiter.Error:
		// The waiter reads with ctx, so it fails as well once ctx ends; report
		// that as the cancellation it is.
		if ctx.Err() == nil {
			return nil, container.abort(fmt.Errorf("wait for %s: %w", spec.Name, err))
		}
	case <-ctx.Done():
	}
	return nil, container.abort(container.WithLogs(fmt.Errorf("wait for %s: %w", spec.Name, context.Cause(ctx))))
}

func create(ctx context.Context, client Client, spec Spec) (*Container, error) {
	archive, err := containerfiles.Archive(spec.Files)
	if err != nil {
		return nil, fmt.Errorf("archive %s files: %w", spec.Name, err)
	}

	config := &containertypes.Config{
		Image:      spec.Image,
		Entrypoint: spec.Entrypoint,
		Env:        spec.Env,
		Labels:     map[string]string{labelKey: spec.Name},
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

type ExitError struct {
	name   string
	status string
}

func (err *ExitError) Error() string {
	return err.name + " container " + err.status
}
