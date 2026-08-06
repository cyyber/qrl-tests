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

// probeHandler serves the execution RPC surface the probe uses: an advancing
// block number and a wallet balance.
func probeHandler(t *testing.T, balanceResult string) http.HandlerFunc {
	t.Helper()
	blockCalls := 0
	return func(writer http.ResponseWriter, request *http.Request) {
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
			fmt.Fprint(writer, `{"jsonrpc":"2.0","id":1,"result":"`+balanceResult+`"}`)
		default:
			t.Fatalf("unexpected RPC method %q", payload.Method)
		}
	}
}

func TestProbeNetwork(t *testing.T) {
	server := httptest.NewServer(probeHandler(t, "0x1"))
	defer server.Close()

	require.NoError(t, probeNetwork(t.Context(), server.URL, devwallet.Address))
}

func TestProbeNetworkRejectsUnfundedWallet(t *testing.T) {
	server := httptest.NewServer(probeHandler(t, "0x0"))
	defer server.Close()

	err := probeNetwork(t.Context(), server.URL, devwallet.Address)
	require.ErrorContains(t, err, "has no balance")
}
