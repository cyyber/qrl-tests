package devnet

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/internal/dockerapi"
	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
)

// isolationImage runs the helper that adds and removes the rules. Only its
// nsenter is used: the helper enters the Docker host's mount namespace for
// that host's nsenter and iptables, and the service's network namespace for
// the rules, so the service's own image needs neither.
const isolationImage = "alpine:3.22"

// isolationTimeout bounds one run of the helper, pulling its image included.
const isolationTimeout = 2 * time.Minute

type serviceIsolator interface {
	containerLister
	imageInspector
	ImagePull(context.Context, string, dockerclient.ImagePullOptions) (dockerclient.ImagePullResponse, error)
	ContainerInspect(context.Context, string, dockerclient.ContainerInspectOptions) (dockerclient.ContainerInspectResult, error)
	ContainerCreate(context.Context, dockerclient.ContainerCreateOptions) (dockerclient.ContainerCreateResult, error)
	ContainerStart(context.Context, string, dockerclient.ContainerStartOptions) (dockerclient.ContainerStartResult, error)
	ContainerWait(context.Context, string, dockerclient.ContainerWaitOptions) dockerclient.ContainerWaitResult
	ContainerRemove(context.Context, string, dockerclient.ContainerRemoveOptions) (dockerclient.ContainerRemoveResult, error)
}

// IsolateService drops every packet between a Kurtosis service's container
// and the peers until the returned rejoin runs, as across a network split.
// Connections stay open: what either side sends meanwhile is retransmitted
// once the rules go. The host-published ports keep working, so the service's
// own clients still reach it. rejoin must run even if the caller fails.
func IsolateService(ctx context.Context, serviceID string, peers []netip.Addr) (func(context.Context) error, error) {
	client, err := dockerapi.New()
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	rejoin, err := isolateService(ctx, client, serviceID, peers)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	return func(ctx context.Context) error {
		return errors.Join(rejoin(ctx), client.Close())
	}, nil
}

func isolateService(
	ctx context.Context, client serviceIsolator, serviceID string, peers []netip.Addr,
) (func(context.Context) error, error) {
	if len(peers) == 0 {
		return nil, fmt.Errorf("isolate service %q: no peers", serviceID)
	}
	container, err := serviceContainer(ctx, client, serviceID)
	if err != nil {
		return nil, fmt.Errorf("find container: %w", err)
	}
	inspected, err := client.ContainerInspect(ctx, container.ID, dockerclient.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("inspect service %q: %w", serviceID, err)
	}
	var pid int
	if inspected.Container.State != nil {
		pid = inspected.Container.State.Pid
	}
	if pid <= 0 {
		return nil, fmt.Errorf("service %q has no running process", serviceID)
	}
	if err := ensureImage(ctx, client, isolationImage); err != nil {
		return nil, err
	}
	if err := runIsolationRules(ctx, client, pid, isolationRules("-I", peers)); err != nil {
		return nil, fmt.Errorf("isolate service %q: %w", serviceID, err)
	}
	return func(ctx context.Context) error {
		if err := runIsolationRules(ctx, client, pid, isolationRules("-D", peers)); err != nil {
			return fmt.Errorf("rejoin service %q: %w", serviceID, err)
		}
		return nil
	}, nil
}

// isolationRules is the script that inserts (-I) or deletes (-D) a rule
// dropping each direction of the traffic with every peer.
func isolationRules(action string, peers []netip.Addr) string {
	rules := make([]string, 0, 2*len(peers))
	for _, peer := range peers {
		rules = append(rules,
			"iptables "+action+" INPUT -s "+peer.String()+" -j DROP",
			"iptables "+action+" OUTPUT -d "+peer.String()+" -j DROP",
		)
	}
	return "set -e\n" + strings.Join(rules, "\n")
}

func ensureImage(ctx context.Context, client serviceIsolator, image string) error {
	if _, err := client.ImageInspect(ctx, image); err == nil {
		return nil
	}
	pulled, err := client.ImagePull(ctx, image, dockerclient.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %q: %w", image, err)
	}
	defer func() { _ = pulled.Close() }()
	if err := pulled.Wait(ctx); err != nil {
		return fmt.Errorf("pull image %q: %w", image, err)
	}
	return nil
}

// runIsolationRules runs script in the network namespace of the process pid,
// with the Docker host's iptables, from a privileged helper container.
func runIsolationRules(ctx context.Context, client serviceIsolator, pid int, script string) error {
	ctx, cancel := context.WithTimeout(ctx, isolationTimeout)
	defer cancel()
	created, err := client.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
		Config: &containertypes.Config{
			Image: isolationImage,
			Cmd: []string{
				"nsenter", "-t", "1", "-m", "--",
				"nsenter", "-t", strconv.Itoa(pid), "-n", "--",
				"sh", "-c", script,
			},
		},
		HostConfig: &containertypes.HostConfig{Privileged: true, PidMode: "host"},
	})
	if err != nil {
		return fmt.Errorf("create helper: %w", err)
	}
	defer func() {
		removeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		_, _ = client.ContainerRemove(removeCtx, created.ID, dockerclient.ContainerRemoveOptions{Force: true})
	}()

	waiter := client.ContainerWait(ctx, created.ID, dockerclient.ContainerWaitOptions{
		Condition: containertypes.WaitConditionNextExit,
	})
	if _, err := client.ContainerStart(ctx, created.ID, dockerclient.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("start helper: %w", err)
	}
	select {
	case status := <-waiter.Result:
		if status.Error != nil {
			return fmt.Errorf("wait for helper: %s", status.Error.Message)
		}
		if status.StatusCode != 0 {
			return fmt.Errorf("helper exited with code %d", status.StatusCode)
		}
		return nil
	case err := <-waiter.Error:
		return fmt.Errorf("wait for helper: %w", err)
	}
}
