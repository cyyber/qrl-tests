package devnet

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/internal/devwallet"
	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/qrlclient"
)

// probeHandler serves the execution RPC surface the probe uses: an advancing
// block number, headers spaced secondsPerSlot apart, and a wallet balance.
func probeHandler(t *testing.T, secondsPerSlot uint64, balanceResult string) http.HandlerFunc {
	t.Helper()
	blockCalls := uint64(0)
	return func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		switch payload.Method {
		case "qrl_blockNumber":
			blockCalls++
			fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":1,"result":"0x%x"}`, blockCalls)
		case "qrl_getBlockByNumber":
			number, err := strconv.ParseUint(strings.TrimPrefix(payload.Params[0].(string), "0x"), 16, 64)
			require.NoError(t, err)
			fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":1,"result":%s}`, headerJSON(number, number*secondsPerSlot))
		case "qrl_getBalance":
			require.Equal(t, []any{devwallet.Address, "latest"}, payload.Params)
			fmt.Fprint(writer, `{"jsonrpc":"2.0","id":1,"result":"`+balanceResult+`"}`)
		default:
			t.Fatalf("unexpected RPC method %q", payload.Method)
		}
	}
}

func headerJSON(number, timestamp uint64) string {
	hash := "0x" + strings.Repeat("0", 64)
	bloom := "0x" + strings.Repeat("0", 512)
	return fmt.Sprintf(
		`{"parentHash":%q,"stateRoot":%q,"transactionsRoot":%q,"receiptsRoot":%q,"logsBloom":%q,"number":"0x%x","gasLimit":"0x1","gasUsed":"0x0","timestamp":"0x%x","extraData":"0x"}`,
		hash, hash, hash, hash, bloom, number, timestamp)
}

func TestProbeNetwork(t *testing.T) {
	server := httptest.NewServer(probeHandler(t, 5, "0x1"))
	defer server.Close()

	require.NoError(t, probeNetwork(t.Context(), server.URL, devwallet.Address))
}

func TestProbeNetworkRejectsUnfundedWallet(t *testing.T) {
	server := httptest.NewServer(probeHandler(t, 5, "0x0"))
	defer server.Close()

	err := probeNetwork(t.Context(), server.URL, devwallet.Address)
	require.ErrorContains(t, err, "has no balance")
}

func TestAdvancementWindowScalesWithSlots(t *testing.T) {
	for name, test := range map[string]struct {
		secondsPerSlot uint64
		head           uint64
		want           time.Duration
	}{
		"fast slots floor": {secondsPerSlot: 5, head: 3, want: minAdvancementWindow},
		"slow slots scale": {secondsPerSlot: 60, head: 3, want: 2 * time.Minute},
		"very slow capped": {secondsPerSlot: 120, head: 3, want: maxAdvancementWindow},
		"no cadence yet":   {secondsPerSlot: 60, head: 0, want: minAdvancementWindow},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(probeHandler(t, test.secondsPerSlot, "0x1"))
			defer server.Close()
			client, err := qrlclient.DialContext(t.Context(), server.URL)
			require.NoError(t, err)
			defer client.Close()

			require.Equal(t, test.want, advancementWindow(t.Context(), client, test.head))
		})
	}
}
