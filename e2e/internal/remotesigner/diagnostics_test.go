package remotesigner

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteDiagnostics(t *testing.T) {
	signer := newSigner(t, map[string]http.HandlerFunc{
		"GET /healthcheck": func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(writer, `{"status":"DOWN","outcome":"DOWN"}`)
		},
		"GET /api/v1/consensus/capabilities": func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, capabilitiesBody)
		},
	})
	metrics := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, metricsFixture)
	}))
	t.Cleanup(metrics.Close)
	validator := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, "go_goroutines 40\nremote_web3signer_errored_responses_total 3\n")
	}))
	t.Cleanup(validator.Close)

	dir := filepath.Join(t.TempDir(), "remote-signer")
	err := WriteDiagnostics(t.Context(), dir, signer.client(t, testToken), metrics.URL, validator.URL)
	require.EqualError(t, err, "healthcheck.json: GET /healthcheck returned 503")

	read := func(name string) string {
		body, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		return string(body)
	}
	require.Equal(t, metricsFixture, read(DiagnosticsMetricsFile))
	require.JSONEq(t, `{"status":"DOWN","outcome":"DOWN"}`, read(DiagnosticsHealthcheckFile))
	require.JSONEq(t, capabilitiesBody, read(DiagnosticsCapabilitiesFile))
	require.Equal(t, "remote_web3signer_errored_responses_total 3\n", read(DiagnosticsValidatorMetricsFile))
	require.Equal(t, "healthcheck.json: GET /healthcheck returned 503\n", read(DiagnosticsErrorsFile))
	for _, request := range signer.requests {
		require.Equal(t, "Bearer "+testToken, request.Header.Get("Authorization"))
	}

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		require.NotContains(t, read(entry.Name()), testToken, entry.Name())
	}
}

func TestWriteDiagnosticsSavesWhatItCanRead(t *testing.T) {
	signer := newSigner(t, map[string]http.HandlerFunc{})
	metrics := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(metrics.Close)

	dir := t.TempDir()
	err := WriteDiagnostics(t.Context(), dir, signer.client(t, testToken), metrics.URL, "")
	require.ErrorContains(t, err, "metrics.txt: GET /metrics returned 404")
	require.ErrorContains(t, err, "validator-metrics.txt: the validator client publishes no metrics port")

	// The unknown routes answer 404, and their bodies are kept.
	for _, name := range []string{DiagnosticsHealthcheckFile, DiagnosticsCapabilitiesFile, DiagnosticsErrorsFile} {
		_, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err, name)
	}
	_, err = os.Stat(filepath.Join(dir, DiagnosticsMetricsFile))
	require.ErrorIs(t, err, os.ErrNotExist)
}
