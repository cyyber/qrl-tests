package health

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// TimerQuantile reads a quantile of a go-qrl timer, such as "chain/inserts"
// at "0.9999", from a metrics page in the Prometheus text format.
func TimerQuantile(ctx context.Context, metricsURL, timer, quantile string) (time.Duration, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, nil)
	if err != nil {
		return 0, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GET %s returned %s", metricsURL, response.Status)
	}

	prefix := strings.ReplaceAll(timer, "/", "_") + ` {quantile="` + quantile + `"} `
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		value, found := strings.CutPrefix(scanner.Text(), prefix)
		if !found {
			continue
		}
		nanoseconds, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0, fmt.Errorf("timer %s at %s: %w", timer, quantile, err)
		}
		return time.Duration(nanoseconds), nil
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("the metrics have no timer %s at quantile %s", timer, quantile)
}
