package soak

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHeadUsesQRLAPIs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			switch request.URL.Path {
			case "/qrl/v1/beacon/headers/head":
				_, _ = writer.Write([]byte(`{"data":{"header":{"message":{"slot":"24"}}}}`))
			case "/qrl/v1/beacon/states/head/finality_checkpoints":
				_, _ = writer.Write([]byte(`{"data":{"current_justified":{"epoch":"2"},"finalized":{"epoch":"1"}}}`))
			case "/qrl/v1/node/peer_count":
				_, _ = writer.Write([]byte(`{"data":{"connected":"0"}}`))
			default:
				http.NotFound(writer, request)
			}
			return
		}
		var payload struct {
			Method string `json:"method"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		switch payload.Method {
		case "qrl_blockNumber":
			_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0xb7"}`))
		case "qrl_getBlockTransactionCountByNumber":
			_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x2"}`))
		case "net_peerCount":
			_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x0"}`))
		case "qrl_syncing":
			_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":false}`))
		case "eth_blockNumber", "eth_syncing":
			t.Errorf("Ethereum RPC method %q is not served by go-qrl", payload.Method)
			http.Error(writer, `{"error":{"code":-32601,"message":"the method does not exist"}}`, http.StatusOK)
		default:
			t.Errorf("unexpected RPC method %q", payload.Method)
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	sample := newProbe(server.Client()).head(t.Context(), Endpoints{
		Index:        1,
		ExecutionRPC: server.URL,
		ConsensusAPI: server.URL,
	})
	require.Empty(t, sample.Faults)
	require.Equal(t, uint64(0xb7), sample.Head)
	require.Equal(t, uint64(2), sample.TxInHead)
	require.Equal(t, uint64(24), sample.HeadSlot)
	require.Equal(t, uint64(1), sample.FinalizedEpoch)
	require.Equal(t, uint64(2), sample.JustifiedEpoch)
	require.False(t, sample.Syncing)
}

func TestBeaconHeadFallsBackToBlocksAndDerivedFinality(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/qrl/v1/beacon/blocks/head":
			_, _ = writer.Write([]byte(`{"data":{"zond_block":{"slot":"40"}}}`))
		case "/qrl/v1/node/peer_count":
			_, _ = writer.Write([]byte(`{"data":{"connected":"0"}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	sample := newProbe(server.Client()).head(t.Context(), Endpoints{
		Index:        1,
		ExecutionRPC: server.URL,
		ConsensusAPI: server.URL,
	})
	require.Equal(t, uint64(40), sample.HeadSlot)
	require.Equal(t, uint64(3), sample.FinalizedEpoch)
	require.Equal(t, uint64(4), sample.JustifiedEpoch)
}

func TestReferenceUsesQRLGetBlockByNumber(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Method string `json:"method"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		require.Equal(t, "qrl_getBlockByNumber", payload.Method)
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"hash":"0xabc","stateRoot":"0xdef"}}`))
	}))
	t.Cleanup(server.Close)

	sample := &ParticipantSample{Index: 1}
	newProbe(server.Client()).reference(t.Context(), Endpoints{ExecutionRPC: server.URL}, sample, 10)
	require.Empty(t, sample.Faults)
	require.Equal(t, "0xabc", sample.ReferenceHash)
	require.Equal(t, "0xdef", sample.ReferenceState)
}


func TestParseExpositionKeepsQuantileLabels(t *testing.T) {
	families, err := parseExposition(strings.NewReader(`
# HELP go_gc_duration_seconds A summary of the pause duration of garbage collection cycles.
go_gc_duration_seconds{quantile="0"} 1e-05
go_gc_duration_seconds{quantile="1"} 0.002
go_gc_duration_seconds_sum 0.1
go_gc_duration_seconds_count 40
process_open_fds 17
`))
	require.NoError(t, err)
	require.InDelta(t, 1e-05, families[`go_gc_duration_seconds`], 1e-12)
	require.InDelta(t, 0.002, families[`go_gc_duration_seconds{quantile="1"}`], 1e-12)
	require.InDelta(t, 40, families["go_gc_duration_seconds_count"], 1e-12)
	require.InDelta(t, 17, firstMetric(families, DefaultThresholds().Metrics.OpenFDs), 1e-12)
	require.InDelta(t, 0.002, firstMetric(families, DefaultThresholds().Metrics.GCPause), 1e-12)
}
