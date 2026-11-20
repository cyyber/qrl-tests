package health

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/internal/dockerapi"
	dockerclient "github.com/moby/moby/client"
)

// ContainerReader inspects a container and reads its logs.
type ContainerReader interface {
	dockerapi.LogReader
	ContainerInspect(context.Context, string, dockerclient.ContainerInspectOptions) (dockerclient.ContainerInspectResult, error)
}

// Container is a container to watch, with its go-qrl metrics page if any.
type Container struct {
	Name       string
	MetricsURL string
}

// droppingMeters are go-qrl meters whose rise means a node dropped or refused
// work.
var droppingMeters = []string{
	"chain_reorg_drop",
	"pathdb_clean_false",
	"pathdb_dirty_false",
	"pathdb_disk_false",
	"qrl_downloader_bodies_drop",
	"qrl_downloader_bodies_timeout",
	"qrl_downloader_headers_drop",
	"qrl_downloader_headers_timeout",
	"qrl_downloader_receipts_drop",
	"qrl_downloader_receipts_timeout",
	"qrl_fetcher_transaction_announces_dos",
	"qrl_fetcher_transaction_request_fail",
	"qrl_fetcher_transaction_request_timeout",
	"qrl_protocols_qrl_egress_handshake_error_forkid",
	"qrl_protocols_qrl_egress_handshake_error_genesis",
	"qrl_protocols_qrl_egress_handshake_error_network",
	"qrl_protocols_qrl_egress_handshake_error_version",
	"qrl_protocols_qrl_ingress_handshake_error_forkid",
	"qrl_protocols_qrl_ingress_handshake_error_genesis",
	"qrl_protocols_qrl_ingress_handshake_error_network",
	"qrl_protocols_qrl_ingress_handshake_error_version",
	"qrl_protocols_snap_egress_registration_error",
	"qrl_protocols_snap_ingress_registration_error",
	"txpool_overflowed",
	"txpool_pending_discard",
	"txpool_pending_nofunds",
	"txpool_pending_ratelimit",
	"txpool_queued_discard",
	"txpool_queued_eviction",
	"txpool_queued_nofunds",
	"txpool_queued_ratelimit",
	"txpool_throttle",
	"txpool_underpriced",
}

var droppingMeterPrefixes = []string{"p2p_lost_"}

var troubleSignatures = []string{
	"panic:",
	"fatal error:",
	"CRIT [",
	"level=fatal",
	"level=panic",
	"message too long",
	"read limit exceeded",
	"degraded performance",
	"BAD BLOCK",
	"invalid merkle root",
	"crashed: ",
	"file an issue",
	"Truncating ancient chain",
	"Head state missing, repairing",
	"Low disk space",
	"Got a validation error in",
	"Pruned invalid blocks",
	"Rolling back insertion of block",
	"could not validate state root",
	"slashing protection check",
	"Unhandled trie error",
	"inserting block failed",
	"Marked new chain head as invalid",
	"Too many bad block import attempt",
	"Failed to build payload",
	"Non contiguous block insert",
	"Gap in the chain, rewinding to genesis",
	"Missing block in the middle",
	"Panic occurred",
	"Panicked",
	"gRPC panicked",
	"Could not update head",
	"Could not compute head from new attestations",
	"Could not finish invalid block cleanup",
	"Could not sign",
	"Failed to sign block",
}

// ContainerWatch records containers' state, then reports what went wrong
// since: an exit or restart, dropped or refused work, or a trouble line in a
// log.
type ContainerWatch struct {
	docker  ContainerReader
	watched []*watched
}

type watched struct {
	Container
	since     time.Time
	startedAt string
	restarts  int
	metrics   map[string]float64
}

func NewContainerWatch(docker ContainerReader) *ContainerWatch {
	return &ContainerWatch{docker: docker}
}

// Watch starts watching containers; Problems reports what changes after.
func (watch *ContainerWatch) Watch(ctx context.Context, containers ...Container) error {
	for _, container := range containers {
		inspected, err := watch.docker.ContainerInspect(ctx, container.Name, dockerclient.ContainerInspectOptions{})
		if err != nil {
			return fmt.Errorf("inspect %s: %w", container.Name, err)
		}
		entry := &watched{Container: container, since: time.Now(), restarts: inspected.Container.RestartCount}
		if state := inspected.Container.State; state != nil {
			entry.startedAt = state.StartedAt
		}
		if container.MetricsURL != "" {
			if entry.metrics, err = readMetrics(ctx, container.MetricsURL); err != nil {
				return fmt.Errorf("%s: %w", container.Name, err)
			}
		}
		watch.watched = append(watch.watched, entry)
	}
	return nil
}

// Problems returns what has gone wrong in the watched containers since Watch.
func (watch *ContainerWatch) Problems(ctx context.Context) ([]string, error) {
	var problems []string
	for _, entry := range watch.watched {
		found, err := watch.problems(ctx, entry)
		for _, problem := range found {
			problems = append(problems, entry.Name+": "+problem)
		}
		if err != nil {
			return problems, fmt.Errorf("%s: %w", entry.Name, err)
		}
	}
	return problems, nil
}

func (watch *ContainerWatch) problems(ctx context.Context, entry *watched) ([]string, error) {
	inspected, err := watch.docker.ContainerInspect(ctx, entry.Name, dockerclient.ContainerInspectOptions{})
	if err != nil {
		return nil, err
	}

	var problems []string
	baseline := entry.metrics
	state := inspected.Container.State
	switch {
	case state == nil:
		problems = append(problems, "has no state")
	case !state.Running:
		problem := fmt.Sprintf("exited with code %d", state.ExitCode)
		if state.OOMKilled {
			problem += ", out of memory"
		}
		problems = append(problems, problem)
	case state.StartedAt != entry.startedAt || inspected.Container.RestartCount != entry.restarts:
		problems = append(problems, "restarted")
		// Its meters began again at zero.
		baseline = nil
	}

	if entry.MetricsURL != "" && state != nil && state.Running {
		values, err := readMetrics(ctx, entry.MetricsURL)
		if err != nil {
			return problems, err
		}
		for _, name := range droppingNames(values, baseline) {
			if rise := values[name] - baseline[name]; rise > 0 {
				problems = append(problems, name+" rose by "+strconv.FormatFloat(rise, 'f', -1, 64))
			}
		}
	}

	lines, err := watch.troubleLines(ctx, entry)
	if err != nil {
		return problems, err
	}
	for _, line := range lines {
		problems = append(problems, "logged "+line)
	}
	return problems, nil
}

// troubleLines returns the container's log lines since Watch that carry a
// trouble signature, at most a few of each.
func (watch *ContainerWatch) troubleLines(ctx context.Context, entry *watched) ([]string, error) {
	output, err := dockerapi.Logs(ctx, watch.docker, entry.Name, dockerclient.ContainerLogsOptions{
		Since: fmt.Sprintf("%d.%09d", entry.since.Unix(), entry.since.Nanosecond()),
	})
	if err != nil {
		return nil, err
	}

	const perSignature = 3
	seen := make(map[string]int)
	var lines []string
	for line := range strings.Lines(output) {
		index := slices.IndexFunc(troubleSignatures, func(signature string) bool {
			return strings.Contains(line, signature)
		})
		if index < 0 {
			continue
		}
		signature := troubleSignatures[index]
		if seen[signature]++; seen[signature] <= perSignature {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return lines, nil
}

// droppingNames returns the dropping meters to compare, those of the prefixed
// families included, in order.
func droppingNames(readings ...map[string]float64) []string {
	names := slices.Clone(droppingMeters)
	for _, values := range readings {
		for name := range values {
			if slices.ContainsFunc(droppingMeterPrefixes, func(prefix string) bool {
				return strings.HasPrefix(name, prefix)
			}) {
				names = append(names, name)
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}
