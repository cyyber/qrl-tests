package health

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGaugeAndSummaryMean(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("txpool_slots 6144\nfast_sum 120\nfast_count 5\nempty_sum 0\nempty_count 0\n"))
	}))
	defer server.Close()

	slots, err := Gauge(t.Context(), server.URL, "txpool_slots")
	require.NoError(t, err)
	require.InDelta(t, 6144.0, slots, 1e-9)
	_, err = Gauge(t.Context(), server.URL, "missing")
	require.ErrorContains(t, err, "no missing")

	mean, err := SummaryMean(t.Context(), server.URL, "fast")
	require.NoError(t, err)
	require.InDelta(t, 24.0, mean, 1e-9)
	mean, err = SummaryMean(t.Context(), server.URL, "empty")
	require.NoError(t, err)
	require.Zero(t, mean)
}
