package devnet

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyyber/qrl-tests/internal/devwallet"
	"github.com/stretchr/testify/require"
)

func TestProbeNetwork(t *testing.T) {
	blockCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Method string   `json:"method"`
			Params []string `json:"params"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		switch payload.Method {
		case "qrl_blockNumber":
			blockCalls++
			fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":1,"result":"0x%x"}`, blockCalls)
		case "qrl_getBalance":
			require.Equal(t, []string{devwallet.Address, "latest"}, payload.Params)
			fmt.Fprint(writer, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
		default:
			t.Fatalf("unexpected RPC method %q", payload.Method)
		}
	}))
	defer server.Close()

	require.NoError(t, probeNetwork(t.Context(), server.URL, devwallet.Address))
	require.GreaterOrEqual(t, blockCalls, 2)
}
