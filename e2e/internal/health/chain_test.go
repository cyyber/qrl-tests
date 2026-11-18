package health

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestSlotHeadVotes(t *testing.T) {
	// Slot 5's committee of four: three vote in block 6, two of them for
	// block 5; the fourth votes in block 7, for another head. Slot 6 has no
	// block.
	blocks := map[string]string{
		"5": `[]`,
		"7": `[{"aggregation_bits":"0x18","data":{"slot":"5","index":"0","beacon_block_root":"0xother"}}]`,
		"8": `[{"aggregation_bits":"0x13","data":{"slot":"5","index":"0","beacon_block_root":"0xroot5"}},` +
			`{"aggregation_bits":"0x14","data":{"slot":"5","index":"0","beacon_block_root":"0xother"}}]`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		slot := request.URL.Path[strings.LastIndex(request.URL.Path, "/")+1:]
		if slot == "head" {
			slot = "8"
		}
		attestations, proposed := blocks[slot]
		switch {
		case !proposed:
			http.NotFound(writer, request)
		case strings.Contains(request.URL.Path, "/headers/"):
			_, _ = fmt.Fprintf(writer, `{"data":{"root":"0xroot%s","header":{"message":{"slot":"%s"}}}}`, slot, slot)
		default:
			_, _ = fmt.Fprintf(writer, `{"data":{"message":{"slot":"%s","proposer_index":"1","body":{"attestations":%s,"execution_payload":{"block_number":"1","withdrawals":[]}}}}}`, slot, attestations)
		}
	}))
	defer server.Close()
	client, err := beacon.New(server.URL)
	require.NoError(t, err)

	votes, err := SlotHeadVotes(t.Context(), client, 5, 6, 3)
	require.NoError(t, err)
	require.Equal(t, []HeadVotes{{Slot: 5, Voted: 4, ForBlock: 2}}, votes)
}
