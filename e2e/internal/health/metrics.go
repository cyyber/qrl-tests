package health

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// metricsURL returns the URL of the go-qrl metrics page that a container
// serves on port, published to the host.
func metricsURL(ctx context.Context, docker ContainerReader, container string, port uint16) (string, error) {
	hostPort, err := PublishedPort(ctx, docker, container, port)
	if err != nil {
		return "", err
	}
	return "http://127.0.0.1:" + hostPort + "/debug/metrics/prometheus", nil
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
