package health

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadMetrics(t *testing.T) {
	serve := func(t *testing.T, status int, page string) string {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(status)
			_, _ = writer.Write([]byte(page))
		}))
		t.Cleanup(server.Close)
		return server.URL
	}

	t.Run("skips comments, labelled and bad values", func(t *testing.T) {
		page := "# TYPE chain_head gauge\nchain_head 7\nchain_inserts {quantile=\"0.5\"} 9\nnot_a_number x\n"
		values, err := readMetrics(t.Context(), serve(t, http.StatusOK, page))
		require.NoError(t, err)
		require.Equal(t, map[string]float64{"chain_head": 7}, values)
	})
	t.Run("an empty page", func(t *testing.T) {
		_, err := readMetrics(t.Context(), serve(t, http.StatusOK, "# nothing\n"))
		require.ErrorContains(t, err, "has no values")
	})
	t.Run("a failed request", func(t *testing.T) {
		_, err := readMetrics(t.Context(), serve(t, http.StatusServiceUnavailable, ""))
		require.ErrorContains(t, err, "503")
	})
}
