package health

import (
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
