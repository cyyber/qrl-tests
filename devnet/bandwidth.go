package devnet

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/cyyber/qrl-tests/internal/dockerapi"
	dockerclient "github.com/moby/moby/client"
)

// LimitBandwidth limits a Docker container's traffic with the peers to rate,
// such as "10mbit", in each direction, until restore runs: what it sends them
// is shaped, and what they send it over the rate is dropped. Its traffic with
// anything else is left alone, as a node's link to the network is slow while
// the clients on its own machine are not. The limit is lost if the container
// restarts. Call PrepareIsolation first. restore must run even if the caller
// fails.
func LimitBandwidth(ctx context.Context, container, rate string, peers []netip.Addr) (func(context.Context) error, error) {
	client, err := dockerapi.New()
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	restore, err := limitBandwidth(ctx, client, container, rate, peers)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	return func(ctx context.Context) error {
		return errors.Join(restore(ctx), client.Close())
	}, nil
}

func limitBandwidth(
	ctx context.Context, client serviceIsolator, container, rate string, peers []netip.Addr,
) (func(context.Context) error, error) {
	if len(peers) == 0 {
		return nil, fmt.Errorf("limit the bandwidth of container %q: no peers", container)
	}
	inspected, err := client.ContainerInspect(ctx, container, dockerclient.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("inspect container %q: %w", container, err)
	}
	var pid int
	if inspected.Container.State != nil {
		pid = inspected.Container.State.Pid
	}
	if pid <= 0 {
		return nil, fmt.Errorf("container %q has no running process", container)
	}
	if err := runIsolationRules(ctx, client, pid, limitRules(rate, peers)); err != nil {
		err = fmt.Errorf("limit the bandwidth of container %q: %w", container, err)
		if cleanupErr := runIsolationRules(context.WithoutCancel(ctx), client, pid, unlimitRules); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove the limit of container %q: %w", container, cleanupErr))
		}
		return nil, err
	}
	return func(ctx context.Context) error {
		if err := runIsolationRules(ctx, client, pid, unlimitRules); err != nil {
			return fmt.Errorf("remove the bandwidth limit of container %q: %w", container, err)
		}
		return nil
	}, nil
}

// limitRules sends the traffic to the peers through a token bucket of its
// own, and polices the traffic from them, since the kernel of Docker Desktop
// has no ifb device to shape it. The peers share the rate.
func limitRules(rate string, peers []netip.Addr) string {
	rules := []string{
		"set -e",
		// Three bands that take all other traffic, and a fourth for the peers.
		"tc qdisc add dev eth0 root handle 1: prio bands 4",
		"tc qdisc add dev eth0 parent 1:4 handle 40: tbf rate " + rate + " burst 64kb latency 100ms",
		"tc qdisc add dev eth0 handle ffff: ingress",
	}
	for _, peer := range peers {
		address := peer.String() + "/32"
		rules = append(rules,
			"tc filter add dev eth0 parent 1: protocol ip u32 match ip dst "+address+" flowid 1:4",
			"tc filter add dev eth0 parent ffff: protocol ip u32 match ip src "+address+
				" action police rate "+rate+" burst 256k conform-exceed drop",
		)
	}
	return strings.Join(rules, "\n")
}

const unlimitRules = "tc qdisc del dev eth0 root || true\ntc qdisc del dev eth0 ingress || true"
