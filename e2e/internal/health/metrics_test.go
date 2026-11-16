package health

import (
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTimerQuantile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("# TYPE chain_inserts_count counter\n" +
			"chain_inserts_count 12\n" +
			"chain_inserts {quantile=\"0.5\"} 1.5e+06\n" +
			"chain_inserts {quantile=\"0.9999\"} 2.5e+08\n"))
	}))
	defer server.Close()

	slowest, err := TimerQuantile(t.Context(), server.URL, "chain/inserts", "0.9999")
	require.NoError(t, err)
	require.Equal(t, 250*time.Millisecond, slowest)

	_, err = TimerQuantile(t.Context(), server.URL, "chain/execution", "0.9999")
	require.ErrorContains(t, err, "no timer chain/execution")
}

func TestHistogramCeiling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`fast_bucket{le="25"} 3
fast_bucket{le="50"} 5
fast_bucket{le="100"} 5
fast_bucket{le="+Inf"} 5
fast_sum 120
fast_count 5
slow_bucket{le="25"} 1
slow_bucket{le="+Inf"} 2
slow_count 2
empty_count 0
`))
	}))
	defer server.Close()

	for histogram, want := range map[string]float64{"fast": 50, "slow": math.Inf(1), "empty": 0} {
		ceiling, err := HistogramCeiling(t.Context(), server.URL, histogram)
		require.NoError(t, err, histogram)
		require.Equal(t, want, ceiling, histogram)
	}
	_, err := HistogramCeiling(t.Context(), server.URL, "missing")
	require.ErrorContains(t, err, "no histogram missing")
}
