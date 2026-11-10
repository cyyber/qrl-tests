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
// nsenter is used, to run the Docker host's iptables in the service's network,
// so neither image needs iptables.
const isolationImage = "alpine:3.22@sha256:49340b346798875b5592e5d420241f2f9cfba5f47e62ebb1446a1b34734e898f"

const isolationTimeout = 2 * time.Minute

type imagePuller interface {
	imageInspector
	ImagePull(context.Context, string, dockerclient.ImagePullOptions) (dockerclient.ImagePullResponse, error)
}

type serviceIsolator interface {
	containerLister
	imagePuller
	ContainerInspect(context.Context, string, dockerclient.ContainerInspectOptions) (dockerclient.ContainerInspectResult, error)
	ContainerCreate(context.Context, dockerclient.ContainerCreateOptions) (dockerclient.ContainerCreateResult, error)
	ContainerStart(context.Context, string, dockerclient.ContainerStartOptions) (dockerclient.ContainerStartResult, error)
	ContainerWait(context.Context, string, dockerclient.ContainerWaitOptions) dockerclient.ContainerWaitResult
	ContainerRemove(context.Context, string, dockerclient.ContainerRemoveOptions) (dockerclient.ContainerRemoveResult, error)
}

// PrepareIsolation pulls the helper IsolateService runs, so that a first
// isolation timed to a slot doesn't wait for the pull.
func PrepareIsolation(ctx context.Context) error {
	client, err := dockerapi.New()
	if err != nil {
		return fmt.Errorf("create Docker client: %w", err)
	}
	return errors.Join(ensureImage(ctx, client, isolationImage), client.Close())
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
		return nil, cleanUpAfter(ctx, client, pid, serviceID, peers, fmt.Errorf("isolate service %q: %w", serviceID, err))
	}
	return func(ctx context.Context) error {
		if err := runIsolationRules(ctx, client, pid, isolationRules("-D", peers)); err != nil {
			return cleanUpAfter(ctx, client, pid, serviceID, peers, fmt.Errorf("rejoin service %q: %w", serviceID, err))
		}
		return nil
	}, nil
}

// cleanUpAfter deletes whichever of the rules exist after failed, an insert
// or delete that may have stopped partway and left some in place. It returns
// failed, with the cleanup's own error if it fails too.
func cleanUpAfter(
	ctx context.Context, client serviceIsolator, pid int, serviceID string, peers []netip.Addr, failed error,
) error {
	if err := runIsolationRules(context.WithoutCancel(ctx), client, pid, cleanupRules(peers)); err != nil {
		return errors.Join(failed, fmt.Errorf("remove the rules of service %q: %w", serviceID, err))
	}
	return failed
}

// isolationRules is the script that inserts (-I) or deletes (-D) a rule
// dropping each direction of the traffic with every peer.
func isolationRules(action string, peers []netip.Addr) string {
	return "set -e\n" + strings.Join(peerRules(action, peers), "\n")
}

// cleanupRules is the script that deletes whichever of the rules exist.
func cleanupRules(peers []netip.Addr) string {
	rules := peerRules("-D", peers)
	for position := range rules {
		rules[position] += " || true"
	}
	return strings.Join(rules, "\n")
}

func peerRules(action string, peers []netip.Addr) []string {
	rules := make([]string, 0, 2*len(peers))
	for _, peer := range peers {
		rules = append(rules,
			"iptables "+action+" INPUT -s "+peer.String()+" -j DROP",
			"iptables "+action+" OUTPUT -d "+peer.String()+" -j DROP",
		)
	}
	return rules
}

func ensureImage(ctx context.Context, client imagePuller, image string) error {
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
