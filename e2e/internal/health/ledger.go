package health

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/network"
	dockerclient "github.com/moby/moby/client"
)

// ContainerReader is the part of the Docker client the ledger uses.
type ContainerReader interface {
	ContainerInspect(context.Context, string, dockerclient.ContainerInspectOptions) (dockerclient.ContainerInspectResult, error)
	ContainerLogs(context.Context, string, dockerclient.ContainerLogsOptions) (dockerclient.ContainerLogsResult, error)
}

// Container is a node's container for the ledger to watch.
type Container struct {
	// Name is the container's Docker name or ID.
	Name string
	// Metrics is the URL of the node's go-qrl metrics page, if it has one.
	Metrics string
}

// Meters whose rise means a node dropped or refused work: go-qrl's transaction
// pool, transaction fetcher, downloader and chain.
var droppingMeters = []string{
	"chain_reorg_drop",
	"qrl_downloader_bodies_drop",
	"qrl_downloader_bodies_timeout",
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

// Ledger records the state of a network's containers when a suite starts
// loading them, and reports what has gone wrong since: a container that
// exited or restarted, a node that dropped or refused work, a crash or size
// refusal in a log.
type Ledger struct {
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

func NewLedger(docker ContainerReader) *Ledger {
	return &Ledger{docker: docker}
}

// Watch records the containers' state as the baseline for Problems.
func (ledger *Ledger) Watch(ctx context.Context, containers ...Container) error {
	for _, container := range containers {
		inspected, err := ledger.docker.ContainerInspect(ctx, container.Name, dockerclient.ContainerInspectOptions{})
		if err != nil {
			return fmt.Errorf("inspect %s: %w", container.Name, err)
		}
		entry := &watched{Container: container, since: time.Now(), restarts: inspected.Container.RestartCount}
		if state := inspected.Container.State; state != nil {
			entry.startedAt = state.StartedAt
		}
		if container.Metrics != "" {
			if entry.meters, err = readMeters(ctx, container.Metrics); err != nil {
				return fmt.Errorf("%s: %w", container.Name, err)
			}
		}
		ledger.watched = append(ledger.watched, entry)
	}
	return nil
}

// Problems returns what has gone wrong in the watched containers since Watch.
func (ledger *Ledger) Problems(ctx context.Context) ([]string, error) {
	var problems []string
	for _, entry := range ledger.watched {
		found, err := ledger.problems(ctx, entry)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name, err)
		}
		for _, problem := range found {
			problems = append(problems, entry.Name+": "+problem)
		}
	}
	return problems, nil
}

func (ledger *Ledger) problems(ctx context.Context, entry *watched) ([]string, error) {
	inspected, err := ledger.docker.ContainerInspect(ctx, entry.Name, dockerclient.ContainerInspectOptions{})
	if err != nil {
		return nil, err
	}
	var problems []string
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
	}

	if entry.Metrics != "" && state != nil && state.Running {
		meters, err := readMeters(ctx, entry.Metrics)
		if err != nil {
			return nil, err
		}
		for _, name := range droppingMeters {
			if rise := meters[name] - entry.meters[name]; rise > 0 {
				problems = append(problems, name+" rose by "+strconv.FormatFloat(rise, 'f', -1, 64))
			}
		}
	}

	lines, err := ledger.troubleLines(ctx, entry)
	if err != nil {
		return nil, err
	}
	for _, line := range lines {
		problems = append(problems, "logged "+line)
	}
	return problems, nil
}

// troubleLines returns the container's log lines since Watch that carry a
// trouble signature, at most a few of each.
func (ledger *Ledger) troubleLines(ctx context.Context, entry *watched) ([]string, error) {
	logs, err := ledger.docker.ContainerLogs(ctx, entry.Name, dockerclient.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Since:      strconv.FormatInt(entry.since.Unix(), 10),
	})
	if err != nil {
		return nil, err
	}
	defer logs.Close()
	var output bytes.Buffer
	if _, err := stdcopy.StdCopy(&output, &output, logs); err != nil {
		return nil, err
	}

	const perSignature = 3
	seen := make(map[string]int)
	var lines []string
	scanner := bufio.NewScanner(&output)
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
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
	return lines, scanner.Err()
}

// MetricsURL returns the URL of the go-qrl metrics page that a container
// serves on port, published to the host.
func MetricsURL(ctx context.Context, docker ContainerReader, container string, port uint16) (string, error) {
	hostPort, err := PublishedPort(ctx, docker, container, port)
	if err != nil {
		return "", err
	}
	return "http://127.0.0.1:" + hostPort + "/debug/metrics/prometheus", nil
}

// PublishedPort returns the host port that a container's TCP port is
// published to.
func PublishedPort(ctx context.Context, docker ContainerReader, container string, port uint16) (string, error) {
	inspected, err := docker.ContainerInspect(ctx, container, dockerclient.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", container, err)
	}
	published, _ := network.PortFrom(port, network.TCP)
	settings := inspected.Container.NetworkSettings
	if settings == nil || len(settings.Ports[published]) == 0 || settings.Ports[published][0].HostPort == "" {
		return "", fmt.Errorf("port %d of %s is not published", port, container)
	}
	return settings.Ports[published][0].HostPort, nil
}

// readMeters reads the unlabelled values of a metrics page in the Prometheus
// text format, by name.
func readMeters(ctx context.Context, url string) (map[string]float64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned %s", url, response.Status)
	}
	meters := make(map[string]float64)
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		name, value, found := strings.Cut(scanner.Text(), " ")
		if !found || strings.HasPrefix(name, "#") || strings.Contains(value, " ") {
			continue
		}
		if number, err := strconv.ParseFloat(value, 64); err == nil {
			meters[name] = number
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(meters) == 0 {
		return nil, errors.New("the metrics page has no values")
	}
	return meters, nil
}

// Gauge reads an unlabelled value, such as "txpool_slots", from a metrics
// page in the Prometheus text format.
func Gauge(ctx context.Context, metricsURL, name string) (float64, error) {
	meters, err := readMeters(ctx, metricsURL)
	if err != nil {
		return 0, err
	}
	value, found := meters[name]
	if !found {
		return 0, fmt.Errorf("the metrics have no %s", name)
	}
	return value, nil
}

// SummaryMean returns the mean observation of a Prometheus summary or
// histogram, or zero for one without observations.
func SummaryMean(ctx context.Context, metricsURL, name string) (float64, error) {
	meters, err := readMeters(ctx, metricsURL)
	if err != nil {
		return 0, err
	}
	count, found := meters[name+"_count"]
	if !found {
		return 0, fmt.Errorf("the metrics have no %s", name)
	}
	if count == 0 {
		return 0, nil
	}
	return meters[name+"_sum"] / count, nil
}
