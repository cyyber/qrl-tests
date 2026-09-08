package soak

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPatchJobAnnotations(t *testing.T) {
	var path, contentType, body string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path = request.URL.Path
		contentType = request.Header.Get("Content-Type")
		payload, _ := io.ReadAll(request.Body)
		body = string(payload)
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	kube := NewKube(server.URL, "qrl", "token", server.Client())
	require.NoError(t, kube.PatchJobAnnotations(t.Context(), "qrl-soak-1", map[string]string{
		"qrl.io/phase": "steady",
	}))
	require.Equal(t, "/apis/batch/v1/namespaces/qrl/jobs/qrl-soak-1", path)
	require.Equal(t, "application/merge-patch+json", contentType)
	require.Contains(t, body, `"qrl.io/phase":"steady"`)
}
