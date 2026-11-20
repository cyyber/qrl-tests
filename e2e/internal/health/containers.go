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

// Container is a node's container to watch, by its Docker name or ID, with
// the URL of its go-qrl metrics page if it has one.
type Container struct {
	Name       string
	MetricsURL string
}

// Meters whose rise means a node dropped or refused work: go-qrl's transaction
// pool, transaction fetcher, downloader and chain.
var droppingMeters = []string{
	"chain_reorg_drop",
	"qrl_downloader_bodies_drop",
	"qrl_downloader_bodies_timeout",
	"qrl_downloader_headers_drop",
	"qrl_downloader_headers_timeout",
	"qrl_downloader_receipts_drop",
	"qrl_downloader_receipts_timeout",
	"qrl_fetcher_transaction_broadcasts_otherreject",
	"qrl_fetcher_transaction_replies_otherreject",
	"qrl_fetcher_transaction_request_timeout",
	"txpool_invalid",
	"txpool_overflowed",
	"txpool_pending_discard",
	"txpool_pending_nofunds",
	"txpool_pending_ratelimit",
	"txpool_queued_discard",
	"txpool_queued_eviction",
	"txpool_queued_nofunds",
	"txpool_queued_ratelimit",
}

// Log lines that mean a node crashed, refused a message for its size, or
// fell behind on its database.
var troubleSignatures = []string{
	"panic:",
	"fatal error:",
	"CRIT [",
	"level=fatal",
	"level=panic",
	"message too long",
	"read limit exceeded",
	"degraded performance",
}

// ContainerWatch records the state of a network's containers when a suite
// starts loading them, and reports what has gone wrong since: a container that
// exited or restarted, a node that dropped or refused work, a trouble line in
// a log.
type ContainerWatch struct {
	docker  ContainerReader
	watched []*watched
}

type watched struct {
	Container
	since     time.Time
	startedAt string
	restarts  int
	meters    map[string]float64
}

func NewContainerWatch(docker ContainerReader) *ContainerWatch {
	return &ContainerWatch{docker: docker}
}

// Watch records the containers' state as the baseline for Problems.
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
			if entry.meters, err = readMetrics(ctx, container.MetricsURL); err != nil {
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
	baseline := entry.meters
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
		meters, err := readMetrics(ctx, entry.MetricsURL)
		if err != nil {
			return problems, err
		}
		for _, name := range droppingMeters {
			if rise := meters[name] - baseline[name]; rise > 0 {
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
