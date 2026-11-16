package health

import (
	"bufio"
	"context"
	"fmt"
	"math"
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

// HistogramCeiling returns the upper bound of the bucket that holds a
// Prometheus histogram's largest observation, which is infinite above the
// last bucket. It is zero for a histogram without observations.
func HistogramCeiling(ctx context.Context, metricsURL, histogram string) (float64, error) {
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

	type bucket struct{ bound, count float64 }
	var buckets []bucket
	total, found := 0.0, false
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if value, isCount := strings.CutPrefix(line, histogram+"_count "); isCount {
			if total, err = strconv.ParseFloat(value, 64); err != nil {
				return 0, fmt.Errorf("histogram %s count: %w", histogram, err)
			}
			found = true
			continue
		}
		rest, isBucket := strings.CutPrefix(line, histogram+`_bucket{le="`)
		if !isBucket {
			continue
		}
		bound, count, _ := strings.Cut(rest, `"} `)
		var parsed bucket
		if parsed.bound, err = strconv.ParseFloat(bound, 64); err != nil {
			return 0, fmt.Errorf("histogram %s bucket %q: %w", histogram, bound, err)
		}
		if parsed.count, err = strconv.ParseFloat(count, 64); err != nil {
			return 0, fmt.Errorf("histogram %s bucket %q: %w", histogram, bound, err)
		}
		buckets = append(buckets, parsed)
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("the metrics have no histogram %s", histogram)
	}
	if total == 0 {
		return 0, nil
	}
	for _, bucket := range buckets {
		if bucket.count >= total {
			return bucket.bound, nil
		}
	}
	return math.Inf(1), nil
}
