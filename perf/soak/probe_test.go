package soak

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseExpositionKeepsQuantileLabels(t *testing.T) {
	families, err := parseExposition(strings.NewReader(`
# HELP go_gc_duration_seconds A summary of the pause duration of garbage collection cycles.
go_gc_duration_seconds{quantile="0"} 1e-05
go_gc_duration_seconds{quantile="1"} 0.002
go_gc_duration_seconds_sum 0.1
go_gc_duration_seconds_count 40
process_open_fds 17
`))
	require.NoError(t, err)
	require.InDelta(t, 1e-05, families[`go_gc_duration_seconds`], 1e-12)
	require.InDelta(t, 0.002, families[`go_gc_duration_seconds{quantile="1"}`], 1e-12)
	require.InDelta(t, 40, families["go_gc_duration_seconds_count"], 1e-12)
	require.InDelta(t, 17, firstMetric(families, DefaultThresholds().Metrics.OpenFDs), 1e-12)
	require.InDelta(t, 0.002, firstMetric(families, DefaultThresholds().Metrics.GCPause), 1e-12)
}
