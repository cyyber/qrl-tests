package graphql

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serve answers every request with status and body. The handler runs on the
// server goroutine, so it uses assert rather than require.
func serve(t *testing.T, status int, body string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var payload map[string]string
		assert.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		assert.Equal(t, map[string]string{"query": "{ block { number } }"}, payload)
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestQueryReturnsData(t *testing.T) {
	url := serve(t, http.StatusOK, `{"data":{"block":{"number":"0x5"}}}`)
	data, err := Query(t.Context(), url, "{ block { number } }")
	require.NoError(t, err)
	require.Equal(t, map[string]any{"block": map[string]any{"number": "0x5"}}, data)
}

func TestQueryFailsOnGraphQLErrors(t *testing.T) {
	url := serve(t, http.StatusBadRequest, `{"errors":[{"message":"Cannot query field \"nope\""}],"data":null}`)
	_, err := Query(t.Context(), url, "{ block { number } }")
	require.ErrorContains(t, err, `Cannot query field \"nope\"`)
}

func TestQueryFailsOnAnErrorStatus(t *testing.T) {
	url := serve(t, http.StatusInternalServerError, `{"data":null}`)
	_, err := Query(t.Context(), url, "{ block { number } }")
	require.ErrorContains(t, err, "500 Internal Server Error")

	url = serve(t, http.StatusBadGateway, `bad gateway`)
	_, err = Query(t.Context(), url, "{ block { number } }")
	require.ErrorContains(t, err, "502 Bad Gateway")
}
