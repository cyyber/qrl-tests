package remotesigner

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A scrape with every label value of go-remotesigner's docs/contract.md,
// section "Metric label values", most at zero as the signer exports them from
// startup, and the 503s of /upcheck that a readiness probe polling it while
// the signer loaded its keys leaves behind.
const metricsFixture = `# HELP signing_requests_total Sign requests by artifact type and outcome
# TYPE signing_requests_total counter
signing_requests_total{result="ok",type="ATTESTATION"} 120
signing_requests_total{result="refused",type="ATTESTATION"} 1
signing_requests_total{result="bad_request",type="ATTESTATION"} 0
signing_requests_total{result="error",type="ATTESTATION"} 0
signing_requests_total{result="ok",type="BLOCK_V2"} 14
signing_requests_total{result="refused",type="BLOCK_V2"} 0
signing_requests_total{result="ok",type="AGGREGATION_SLOT"} 120
signing_requests_total{result="ok",type="VOLUNTARY_EXIT"} 0
# HELP consensus_randao_reveals_total RANDAO reveal requests by outcome
# TYPE consensus_randao_reveals_total counter
consensus_randao_reveals_total{result="first_use"} 13
consensus_randao_reveals_total{result="new_layer"} 1
consensus_randao_reveals_total{result="gap_advanced"} 0
consensus_randao_reveals_total{result="re_reveal"} 0
consensus_randao_reveals_total{result="refused"} 0
consensus_randao_reveals_total{result="error"} 0
# HELP consensus_randao_new_layer_rate_limited_total Reveals refused because the key's new-layer bucket was empty
# TYPE consensus_randao_new_layer_rate_limited_total counter
consensus_randao_new_layer_rate_limited_total 0
# HELP consensus_slashingprotection_prevented_signings_total Block and attestation checks that refused signing
# TYPE consensus_slashingprotection_prevented_signings_total counter
consensus_slashingprotection_prevented_signings_total{reason="slashing_double_proposal"} 0
consensus_slashingprotection_prevented_signings_total{reason="slashing_double_vote"} 1
consensus_slashingprotection_prevented_signings_total{reason="slashing_surrounding_vote"} 0
consensus_slashingprotection_prevented_signings_total{reason="slashing_surrounded_vote"} 0
consensus_slashingprotection_prevented_signings_total{reason="slashing_below_watermark"} 0
consensus_slashingprotection_prevented_signings_total{reason="slashing_above_high_watermark"} 0
consensus_slashingprotection_prevented_signings_total{reason="validator_disabled"} 0
# HELP signing_verify_failures_total Signatures that failed verify-before-release
# TYPE signing_verify_failures_total counter
signing_verify_failures_total 0
# HELP http_request_duration_seconds API requests by route pattern and status
# TYPE http_request_duration_seconds histogram
http_request_duration_seconds_bucket{code="200",route="/api/v1/consensus/sign/{identifier}",le="0.1"} 250
http_request_duration_seconds_bucket{code="200",route="/api/v1/consensus/sign/{identifier}",le="+Inf"} 254
http_request_duration_seconds_sum{code="200",route="/api/v1/consensus/sign/{identifier}"} 3.5
http_request_duration_seconds_count{code="200",route="/api/v1/consensus/sign/{identifier}"} 254
http_request_duration_seconds_bucket{code="412",route="/api/v1/consensus/sign/{identifier}",le="+Inf"} 1
http_request_duration_seconds_sum{code="412",route="/api/v1/consensus/sign/{identifier}"} 0.01
http_request_duration_seconds_count{code="412",route="/api/v1/consensus/sign/{identifier}"} 1
http_request_duration_seconds_bucket{code="503",route="/upcheck",le="+Inf"} 2
http_request_duration_seconds_sum{code="503",route="/upcheck"} 0.001
http_request_duration_seconds_count{code="503",route="/upcheck"} 2
http_request_duration_seconds_bucket{code="200",route="/upcheck",le="+Inf"} 1
http_request_duration_seconds_sum{code="200",route="/upcheck"} 0.001
http_request_duration_seconds_count{code="200",route="/upcheck"} 1
`

func TestMetricsSum(t *testing.T) {
	metrics := must(ParseMetrics([]byte(metricsFixture)))

	require.Equal(t, 120.0, must(metrics.Sum(MetricSigningRequests, With("type", TypeAttestation, "result", "ok"))))
	require.Equal(t, 254.0, must(metrics.Sum(MetricSigningRequests, With("result", "ok"))))
	require.Equal(t, 255.0, must(metrics.Sum(MetricSigningRequests, nil)))
	require.Equal(t, 14.0, must(metrics.Sum(MetricRandaoReveals, OneOf("result", "first_use", "new_layer"))))
	require.Equal(t, 0.0, must(metrics.Sum(MetricRandaoRateLimited, nil)))
	require.Equal(t, 1.0, must(metrics.Sum(MetricPreventedSignings, nil)))
	require.Equal(t, 0.0, must(metrics.Sum(MetricVerifyFailures, nil)))
	// A histogram contributes its count of observations.
	require.Equal(t, 258.0, must(metrics.Sum(MetricHTTPRequestDurations, nil)))
	require.Equal(t, 1.0, must(metrics.Sum(MetricHTTPRequestDurations, With("code", "412"))))
	// The 503s of /upcheck while the signer loaded its keys are no fault.
	require.Equal(t, 2.0, must(metrics.Sum(MetricHTTPRequestDurations, With("code", "503"))))
	require.Equal(t, 0.0, must(metrics.Sum(MetricHTTPRequestDurations, ServerFaults())))

	_, err := metrics.Sum("signing_requests", nil)
	require.EqualError(t, err, "the scrape has no metric signing_requests")

	require.Equal(t,
		map[string]float64{"ATTESTATION": 121, "BLOCK_V2": 14, "AGGREGATION_SLOT": 120, "VOLUNTARY_EXIT": 0},
		metrics.ByLabel(MetricSigningRequests, "type", nil),
	)
	require.Equal(t,
		map[string]float64{"ATTESTATION": 120, "BLOCK_V2": 14, "AGGREGATION_SLOT": 120, "VOLUNTARY_EXIT": 0},
		metrics.ByLabel(MetricSigningRequests, "type", With("result", "ok")),
	)
	require.Empty(t, metrics.ByLabel("absent_total", "type", nil))
}

func TestServerFaults(t *testing.T) {
	match := ServerFaults()
	sign := "/api/v1/consensus/sign/{identifier}"
	require.True(t, match(Labels{"code": "500", "route": sign}))
	require.True(t, match(Labels{"code": "503", "route": sign}))
	require.True(t, match(Labels{"code": "503", "route": "-"}))
	require.True(t, match(Labels{"code": "503", "route": "/healthcheck"}))
	require.True(t, match(Labels{"code": "500"}))
	// /upcheck answers 503 LOADING until the keys are loaded, but any other
	// 5xx of it is a fault.
	require.False(t, match(Labels{"code": "503", "route": "/upcheck"}))
	require.True(t, match(Labels{"code": "500", "route": "/upcheck"}))
	require.False(t, match(Labels{"code": "200", "route": "/upcheck"}))
	require.False(t, match(Labels{"code": "412", "route": sign}))
	require.False(t, match(Labels{"code": "5"}))
	require.False(t, match(Labels{}))
}

func TestScrape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/metrics" {
			http.NotFound(writer, request)
			return
		}
		_, _ = io.WriteString(writer, metricsFixture)
	}))
	t.Cleanup(server.Close)

	metrics := must(Scrape(t.Context(), server.URL))
	require.Equal(t, 1.0, must(metrics.Sum(MetricPreventedSignings, With("reason", "slashing_double_vote"))))
	require.Equal(t, metricsFixture, string(must(ScrapeText(t.Context(), server.URL+"/"))))

	_, err := Scrape(t.Context(), server.URL+"/missing")
	require.EqualError(t, err, "GET /metrics returned 404")

	_, err = ParseMetrics([]byte("not { metrics"))
	require.ErrorContains(t, err, "parse metrics")
}

func TestMetricLines(t *testing.T) {
	scrape := strings.Join([]string{
		"# HELP remote_web3signer_sign_requests_total Total number of sign requests",
		"# TYPE remote_web3signer_sign_requests_total counter",
		"remote_web3signer_sign_requests_total 12",
		"# HELP go_goroutines Number of goroutines",
		"go_goroutines 40",
		"remote_web3signer_errored_responses_total 0",
		"",
	}, "\n")
	require.Equal(t, strings.Join([]string{
		"# HELP remote_web3signer_sign_requests_total Total number of sign requests",
		"# TYPE remote_web3signer_sign_requests_total counter",
		"remote_web3signer_sign_requests_total 12",
		"remote_web3signer_errored_responses_total 0",
		"",
	}, "\n"), string(MetricLines([]byte(scrape), "remote_web3signer_")))
}
