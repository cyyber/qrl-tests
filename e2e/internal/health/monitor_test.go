package health

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

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

type fakeStats string

func (stats fakeStats) ContainerStats(_ context.Context, _ string, _ dockerclient.ContainerStatsOptions) (dockerclient.ContainerStatsResult, error) {
	return dockerclient.ContainerStatsResult{Body: io.NopCloser(strings.NewReader(string(stats)))}, nil
}

func TestMemoryUsage(t *testing.T) {
	usage, err := MemoryUsage(t.Context(), fakeStats(`{"memory_stats":{"usage":123456789,"limit":1}}`), "node")
	require.NoError(t, err)
	require.Equal(t, uint64(123456789), usage)

	_, err = MemoryUsage(t.Context(), fakeStats(`{}`), "node")
	require.ErrorContains(t, err, "no memory usage")
}
