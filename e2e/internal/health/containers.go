package health

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/internal/dockerapi"
	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
)

// ContainerReader inspects a container and reads its logs.
type ContainerReader interface {
	dockerapi.LogReader
	ContainerInspect(context.Context, string, dockerclient.ContainerInspectOptions) (dockerclient.ContainerInspectResult, error)
}

// Container is a container to monitor, with its go-qrl metrics page if any.
type Container struct {
	Name       string
	MetricsURL string
}

// troubleMeters are go-qrl meters whose rise means trouble: dropped, refused
// or lost work, corrupt state, or a mismatched peer.
var troubleMeters = []string{
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

var troubleMeterPrefixes = []string{"p2p_lost_"}

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

// containerStates record containers' state, then report what went wrong since:
// an exit or restart, dropped or refused work, or a trouble line in a log.
type containerStates struct {
	docker          ContainerReader
	recorded        []*containerState
	retiredProblems []string
}

type containerState struct {
	Container
	since     time.Time
	startedAt string
	metrics   map[string]float64
}

func (states *containerStates) record(ctx context.Context, containers ...Container) error {
	for _, container := range containers {
		inspected, err := states.docker.ContainerInspect(ctx, container.Name, dockerclient.ContainerInspectOptions{})
		if err != nil {
			return fmt.Errorf("inspect %s: %w", container.Name, err)
		}
		entry := &containerState{Container: container, since: time.Now()}
		if state := inspected.Container.State; state != nil {
			entry.startedAt = state.StartedAt
		}
		if container.MetricsURL != "" {
			if entry.metrics, err = readMetrics(ctx, container.MetricsURL); err != nil {
				return fmt.Errorf("%s: %w", container.Name, err)
			}
		}
		states.recorded = append(states.recorded, entry)
	}
	return nil
}

// retire takes a container's final reading, keeps its problems and stops
// inspecting it.
func (states *containerStates) retire(ctx context.Context, name string) error {
	index := slices.IndexFunc(states.recorded, func(entry *containerState) bool { return entry.Name == name })
	if index < 0 {
		return fmt.Errorf("%s is not monitored", name)
	}
	entry := states.recorded[index]
	states.recorded = slices.Delete(states.recorded, index, index+1)

	found, err := states.check(ctx, entry)
	states.retiredProblems = append(states.retiredProblems, found...)
	return err
}

// problems returns what has gone wrong since each container was recorded.
func (states *containerStates) problems(ctx context.Context) ([]string, error) {
	problems := slices.Clone(states.retiredProblems)
	for _, entry := range states.recorded {
		found, err := states.check(ctx, entry)
		problems = append(problems, found...)
		if err != nil {
			return problems, err
		}
	}
	return problems, nil
}

// check inspects a container and prefixes its problems and error with its name.
func (states *containerStates) check(ctx context.Context, entry *containerState) ([]string, error) {
	found, err := states.inspect(ctx, entry)
	for index, problem := range found {
		found[index] = entry.Name + ": " + problem
	}
	if err != nil {
		return found, fmt.Errorf("%s: %w", entry.Name, err)
	}
	return found, nil
}

func (states *containerStates) inspect(ctx context.Context, entry *containerState) ([]string, error) {
	inspected, err := states.docker.ContainerInspect(ctx, entry.Name, dockerclient.ContainerInspectOptions{})
	if err != nil {
		return nil, err
	}

	state := inspected.Container.State
	problems, restarted := stateProblems(state, entry.startedAt)
	if state != nil && state.Running && entry.MetricsURL != "" {
		rises, err := meterRises(ctx, entry, restarted)
		problems = append(problems, rises...)
		if err != nil {
			return problems, err
		}
	}

	lines, err := states.troubleLines(ctx, entry)
	for _, line := range lines {
		problems = append(problems, "logged "+line)
	}
	return problems, err
}

// stateProblems returns what a container's state shows went wrong, and whether
// it restarted.
func stateProblems(state *containertypes.State, startedAt string) ([]string, bool) {
	switch {
	case state == nil:
		return []string{"has no state"}, false
	case !state.Running:
		problem := fmt.Sprintf("exited with code %d", state.ExitCode)
		if state.OOMKilled {
			problem += ", out of memory"
		}
		return []string{problem}, false
	case state.StartedAt != startedAt:
		return []string{"restarted"}, true
	}
	return nil, false
}

// meterRises returns the trouble meters that rose since entry was recorded,
// counted from zero after a restart.
func meterRises(ctx context.Context, entry *containerState, restarted bool) ([]string, error) {
	values, err := readMetrics(ctx, entry.MetricsURL)
	if err != nil {
		return nil, err
	}

	baseline := entry.metrics
	if restarted {
		baseline = nil
	}
	var rises []string
	for _, name := range troubleMetersIn(values, baseline) {
		if rise := values[name] - baseline[name]; rise > 0 {
			rises = append(rises, name+" rose by "+strconv.FormatFloat(rise, 'f', -1, 64))
		}
	}
	return rises, nil
}

// troubleLines returns the trouble lines logged since entry was recorded.
func (states *containerStates) troubleLines(ctx context.Context, entry *containerState) ([]string, error) {
	output, err := dockerapi.Logs(ctx, states.docker, entry.Name, dockerclient.ContainerLogsOptions{
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

// troubleMetersIn returns the trouble meters in readings, prefixed families
// included, sorted.
func troubleMetersIn(readings ...map[string]float64) []string {
	names := slices.Clone(troubleMeters)
	for _, values := range readings {
		for name := range values {
			if slices.ContainsFunc(troubleMeterPrefixes, func(prefix string) bool {
				return strings.HasPrefix(name, prefix)
			}) {
				names = append(names, name)
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}
