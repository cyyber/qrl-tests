package staking

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/stretchr/testify/require"
)

// fakeBeacon serves the beacon endpoints the staking checks read. Handlers run
// on the server goroutine, so its state is guarded by mu.
type fakeBeacon struct {
	mu       sync.Mutex
	head     uint64
	blocks   map[uint64]string
	failures map[string]int
	requests map[string]int
}

func newFakeBeacon(t *testing.T, head uint64) (*fakeBeacon, *beacon.Client) {
	t.Helper()
	fake := &fakeBeacon{
		head:     head,
		blocks:   map[uint64]string{},
		failures: map[string]int{},
		requests: map[string]int{},
	}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	client, err := beacon.New(server.URL)
	require.NoError(t, err)
	return fake, client
}

func (fake *fakeBeacon) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	path := request.URL.Path
	fake.requests[path]++
	if fake.failures[path] > 0 {
		fake.failures[path]--
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
		return
	}
	number := func(prefix string) uint64 {
		value, _ := strconv.ParseUint(strings.TrimPrefix(path, prefix), 10, 64)
		return value
	}
	var data string
	var ok bool
	switch {
	case path == "/qrl/v1/beacon/headers/head":
		data, ok = fmt.Sprintf(`{"header":{"message":{"slot":"%d"}}}`, fake.head), true
	case strings.HasPrefix(path, "/qrl/v1/beacon/blocks/"):
		data, ok = fake.blocks[number("/qrl/v1/beacon/blocks/")]
	}
	if !ok {
		http.NotFound(writer, request)
		return
	}
	_ = json.NewEncoder(writer).Encode(map[string]json.RawMessage{"data": json.RawMessage(data)})
}

func (fake *fakeBeacon) set(update func(*fakeBeacon)) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	update(fake)
}

func (fake *fakeBeacon) requestCount(path string) int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.requests[path]
}

func block(slot uint64) string {
	return fmt.Sprintf(`{"message":{"slot":"%d","body":{"execution_payload":{"block_number":"%d"}}}}`, slot, slot+100)
}

func TestOperationScanner(t *testing.T) {
	fake, client := newFakeBeacon(t, 13)
	fake.set(func(fake *fakeBeacon) {
		// Slot 11 has no block.
		fake.blocks[12] = block(12)
		fake.blocks[13] = block(13)
		fake.blocks[14] = block(14)
		fake.blocks[15] = block(15)
	})
	scanner := &operationScanner{client: client, lastSlot: 10}
	var visited []uint64
	visit := func(operations beacon.BlockOperations) { visited = append(visited, operations.Slot) }

	require.NoError(t, scanner.scan(t.Context(), visit))
	require.Equal(t, []uint64{12, 13}, visited)
	require.Equal(t, uint64(13), scanner.lastSlot)
	require.Equal(t, uint64(113), scanner.lastBlock)

	// A failed block read is retried by the next scan, without revisiting.
	fake.set(func(fake *fakeBeacon) {
		fake.head = 15
		fake.failures["/qrl/v1/beacon/blocks/15"] = 1
	})
	require.Error(t, scanner.scan(t.Context(), visit))
	require.Equal(t, []uint64{12, 13, 14}, visited)
	require.Equal(t, uint64(14), scanner.lastSlot)

	require.NoError(t, scanner.scan(t.Context(), visit))
	require.Equal(t, []uint64{12, 13, 14, 15}, visited)

	// A missing head slot is not requested again once a later block exists,
	// and has no execution block of its own.
	fake.set(func(fake *fakeBeacon) { fake.head = 16 })
	require.NoError(t, scanner.scan(t.Context(), visit))
	require.Equal(t, uint64(115), scanner.lastBlock)
	fake.set(func(fake *fakeBeacon) {
		fake.head = 17
		fake.blocks[17] = block(17)
	})
	require.NoError(t, scanner.scan(t.Context(), visit))
	require.Equal(t, []uint64{12, 13, 14, 15, 17}, visited)
	require.Equal(t, uint64(117), scanner.lastBlock)
	for _, slot := range []uint64{11, 12, 13, 14, 16, 17} {
		require.Equal(t, 1, fake.requestCount("/qrl/v1/beacon/blocks/"+strconv.FormatUint(slot, 10)), "slot %d", slot)
	}
	// Slot 15 is read twice: the failed read and its retry.
	require.Equal(t, 2, fake.requestCount("/qrl/v1/beacon/blocks/15"))
}

func TestEarnedAny(t *testing.T) {
	require.False(t, earnedAny(beacon.AttestationReward{}))
	require.False(t, earnedAny(beacon.AttestationReward{Source: -5, Target: -5}))
	// A late attestation misses the head reward but still earns the target one.
	require.True(t, earnedAny(beacon.AttestationReward{Target: 7}))
	require.True(t, earnedAny(beacon.AttestationReward{Head: 7, Source: -5, Target: -5}))
}
