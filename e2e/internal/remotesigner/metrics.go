package remotesigner

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

// Metric names of go-remotesigner's docs/contract.md, section "Metric label
// values", which the signer exports from startup, at zero.
const (
	MetricSigningRequests      = "signing_requests_total"
	MetricRandaoReveals        = "consensus_randao_reveals_total"
	MetricRandaoRateLimited    = "consensus_randao_new_layer_rate_limited_total"
	MetricPreventedSignings    = "consensus_slashingprotection_prevented_signings_total"
	MetricVerifyFailures       = "signing_verify_failures_total"
	MetricHTTPRequestDurations = "http_request_duration_seconds"
)

// Metrics of the qrysm validator client's remote signer keymanager; they all
// start with ValidatorMetricPrefix.
const (
	ValidatorMetricPrefix           = "remote_web3signer_"
	ValidatorMetricSignRequests     = "remote_web3signer_sign_requests_total"
	ValidatorMetricRandaoRequests   = "remote_web3signer_randao_reveal_requests_total"
	ValidatorMetricErroredResponses = "remote_web3signer_errored_responses_total"
)

// Labels are the labels of one series.
type Labels map[string]string

// Matcher selects series by their labels.
type Matcher func(Labels) bool

// With matches the series that carry every name=value pair, given as name,
// value, name, value.
func With(pairs ...string) Matcher {
	if len(pairs)%2 != 0 {
		panic("remotesigner.With takes name and value pairs")
	}
	return func(labels Labels) bool {
		for index := 0; index < len(pairs); index += 2 {
			if labels[pairs[index]] != pairs[index+1] {
				return false
			}
		}
		return true
	}
}

// OneOf matches the series whose label name has one of values.
func OneOf(name string, values ...string) Matcher {
	return func(labels Labels) bool {
		value, ok := labels[name]
		return ok && slices.Contains(values, value)
	}
}

// ServerFaults matches the series of http_request_duration_seconds answered
// with a 5xx, but for the 503 LOADING of /upcheck: the signer listens before
// it has loaded its keys, and answers a readiness probe that polls it until
// then with a 503, by design. The route label of /upcheck is its path.
func ServerFaults() Matcher {
	return func(labels Labels) bool {
		code := labels["code"]
		if len(code) != 3 || code[0] != '5' {
			return false
		}
		return labels["route"] != upcheckPath || code != "503"
	}
}

// Metrics is one scrape of a metrics listener.
type Metrics struct {
	families map[string]*dto.MetricFamily
}

// ScrapeText returns the metrics listener's answer as it was served.
func ScrapeText(ctx context.Context, metricsURL string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(metricsURL, "/")+"/metrics", nil)
	if err != nil {
		return nil, err
	}
	// The text format, which the listener serves without negotiation too.
	request.Header.Set("Accept", "text/plain")
	response, err := (&http.Client{Timeout: requestTimeout}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /metrics returned %d", response.StatusCode)
	}
	return body, nil
}

// Scrape reads the metrics listener at metricsURL, its base URL.
func Scrape(ctx context.Context, metricsURL string) (Metrics, error) {
	body, err := ScrapeText(ctx, metricsURL)
	if err != nil {
		return Metrics{}, err
	}
	return ParseMetrics(body)
}

// ParseMetrics parses the Prometheus text format.
func ParseMetrics(body []byte) (Metrics, error) {
	var parser expfmt.TextParser
	families, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	if err != nil {
		return Metrics{}, fmt.Errorf("parse metrics: %w", err)
	}
	return Metrics{families: families}, nil
}

// Sum adds up the series of a metric that match; nil matches them all. A
// counter or gauge contributes its value, a histogram or summary its count
// of observations. A metric the scrape does not have is an error, so a
// renamed metric cannot pass for zero.
func (metrics Metrics) Sum(name string, match Matcher) (float64, error) {
	family, ok := metrics.families[name]
	if !ok {
		return 0, fmt.Errorf("the scrape has no metric %s", name)
	}
	var sum float64
	for _, metric := range family.GetMetric() {
		if match == nil || match(labelsOf(metric)) {
			sum += valueOf(metric)
		}
	}
	return sum, nil
}

// ByLabel sums the matching series of a metric by the value of one label,
// for reports; nil matches them all.
func (metrics Metrics) ByLabel(name, label string, match Matcher) map[string]float64 {
	values := make(map[string]float64)
	for _, metric := range metrics.families[name].GetMetric() {
		labels := labelsOf(metric)
		if match == nil || match(labels) {
			values[labels[label]] += valueOf(metric)
		}
	}
	return values
}

// MetricLines keeps the lines of a text-format scrape, comments included,
// that are about the metrics whose names start with prefix.
func MetricLines(scrape []byte, prefix string) []byte {
	var kept bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(scrape))
	scanner.Buffer(nil, maxBodyBytes)
	for scanner.Scan() {
		line := scanner.Text()
		name := line
		if comment, ok := strings.CutPrefix(line, "# "); ok {
			// # HELP <name> … and # TYPE <name> …
			fields := strings.Fields(comment)
			if len(fields) < 2 {
				continue
			}
			name = fields[1]
		}
		if strings.HasPrefix(name, prefix) {
			kept.WriteString(line)
			kept.WriteByte('\n')
		}
	}
	return kept.Bytes()
}

func valueOf(metric *dto.Metric) float64 {
	switch {
	case metric.Counter != nil:
		return metric.GetCounter().GetValue()
	case metric.Gauge != nil:
		return metric.GetGauge().GetValue()
	case metric.Untyped != nil:
		return metric.GetUntyped().GetValue()
	case metric.Histogram != nil:
		return float64(metric.GetHistogram().GetSampleCount())
	case metric.Summary != nil:
		return float64(metric.GetSummary().GetSampleCount())
	}
	return 0
}

func labelsOf(metric *dto.Metric) Labels {
	labels := make(Labels, len(metric.GetLabel()))
	for _, pair := range metric.GetLabel() {
		labels[pair.GetName()] = pair.GetValue()
	}
	return labels
}
