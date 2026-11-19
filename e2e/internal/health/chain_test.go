package health

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/stretchr/testify/require"
)

func TestMissedSlots(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/qrl/v1/beacon/headers/5", "/qrl/v1/beacon/headers/7":
			_, _ = writer.Write([]byte(`{"data":{"root":"0xab","header":{"message":{"slot":"5"}}}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := beacon.New(server.URL)
	require.NoError(t, err)

	missed, err := MissedSlots(t.Context(), client, 5, 8)
	require.NoError(t, err)
	require.Equal(t, []uint64{6, 8}, missed)
}
