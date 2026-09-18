package staking

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/params"
)

const (
	validatorIndex = 64
	validatorKey   = "0xab"
)

var chain = chaininfo.Info{SlotsPerEpoch: 8}

// fakeBeacon serves the beacon endpoints the staking checks read. Handlers run
// on the server goroutine, so its state is guarded by mu.
type fakeBeacon struct {
	mu       sync.Mutex
	head     uint64
	blocks   map[uint64]string
	duties   map[uint64]string
	rewards  map[uint64]string
	failures map[string]int
	requests map[string]int
}

func newFakeBeacon(t *testing.T, head uint64) (*fakeBeacon, *beacon.Client) {
	t.Helper()
	fake := &fakeBeacon{
		head:     head,
		blocks:   map[uint64]string{},
		duties:   map[uint64]string{},
		rewards:  map[uint64]string{},
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
	case strings.HasPrefix(path, "/qrl/v1/validator/duties/attester/"):
		data, ok = fake.duties[number("/qrl/v1/validator/duties/attester/")]
	case strings.HasPrefix(path, "/qrl/v1/beacon/rewards/attestations/"):
		data, ok = fake.rewards[number("/qrl/v1/beacon/rewards/attestations/")]
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

func block(slot uint64, exits string) string {
	return fmt.Sprintf(`{"message":{"slot":"%d","body":{"voluntary_exits":[%s],"execution_payload":{"block_number":"%d"}}}}`,
		slot, exits, slot+100)
}

func duty(slot uint64, publicKey string) string {
	return fmt.Sprintf(`[{"pubkey":%q,"validator_index":"%d","slot":"%d"}]`, publicKey, validatorIndex, slot)
}

func reward(head int64) string {
	return fmt.Sprintf(`{"total_rewards":[{"validator_index":"%d","head":"%d","target":"0","source":"0"}]}`, validatorIndex, head)
}

func TestOperationScanner(t *testing.T) {
	fake, client := newFakeBeacon(t, 13)
	fake.set(func(fake *fakeBeacon) {
		// Slot 11 has no block.
		fake.blocks[12] = block(12, `{"message":{"epoch":"1","validator_index":"64"},"signature":"0x00"}`)
		fake.blocks[13] = block(13, "")
		fake.blocks[14] = block(14, "")
		fake.blocks[15] = block(15, "")
	})
	scanner := &operationScanner{client: client, lastSlot: 10}
	var visited []uint64
	visit := func(operations beacon.BlockOperations) { visited = append(visited, operations.Slot) }

	require.NoError(t, scanner.scan(t.Context(), visit))
	require.Equal(t, []uint64{12, 13}, visited)
	require.Equal(t, uint64(13), scanner.lastSlot)

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

	// A missing head slot is not requested again once a later block exists.
	fake.set(func(fake *fakeBeacon) { fake.head = 16 })
	require.NoError(t, scanner.scan(t.Context(), visit))
	fake.set(func(fake *fakeBeacon) {
		fake.head = 17
		fake.blocks[17] = block(17, "")
	})
	require.NoError(t, scanner.scan(t.Context(), visit))
	require.Equal(t, []uint64{12, 13, 14, 15, 17}, visited)
	for _, slot := range []uint64{11, 12, 13, 14, 16, 17} {
		require.Equal(t, 1, fake.requestCount("/qrl/v1/beacon/blocks/"+strconv.FormatUint(slot, 10)), "slot %d", slot)
	}
}

func TestFutureAttesterDuty(t *testing.T) {
	// The head is slot 17, in epoch 2.
	for _, testCase := range []struct {
		name     string
		duties   map[uint64]string
		failing  []uint64
		wantSlot uint64
		wantErr  string
	}{
		{name: "later this epoch", duties: map[uint64]string{2: duty(20, validatorKey)}, wantSlot: 20},
		{
			name:     "already passed this epoch",
			duties:   map[uint64]string{2: duty(17, validatorKey), 3: duty(27, validatorKey)},
			wantSlot: 27,
		},
		{
			name:     "another validator's key",
			duties:   map[uint64]string{2: duty(20, "0xcd"), 3: duty(27, validatorKey)},
			wantSlot: 27,
		},
		{
			name:     "slot outside the epoch",
			duties:   map[uint64]string{2: duty(30, validatorKey), 3: duty(27, validatorKey)},
			wantSlot: 27,
		},
		{
			name:    "next epoch unavailable",
			duties:  map[uint64]string{2: duty(16, validatorKey)},
			failing: []uint64{3},
			wantErr: "no future attester duty at head slot 17",
		},
		{name: "current epoch unavailable", failing: []uint64{2}, wantErr: "503"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fake, client := newFakeBeacon(t, 17)
			fake.set(func(fake *fakeBeacon) {
				fake.duties = testCase.duties
				for _, epoch := range testCase.failing {
					fake.failures["/qrl/v1/validator/duties/attester/"+strconv.FormatUint(epoch, 10)] = 1
				}
			})
			got, err := futureAttesterDuty(t.Context(), client, chain, validatorIndex, validatorKey)
			if testCase.wantErr != "" {
				require.ErrorContains(t, err, testCase.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.wantSlot, got.Slot)
		})
	}
}

func TestAttestedFrom(t *testing.T) {
	fake, client := newFakeBeacon(t, 25)
	fake.set(func(fake *fakeBeacon) {
		fake.rewards[1] = reward(0)
		fake.rewards[2] = reward(-5)
		fake.rewards[3] = reward(7)
	})

	// The head is in epoch 3, so rewards are served through epoch 1.
	next, attested, err := attestedFrom(t.Context(), client, chain, validatorIndex, 1)
	require.NoError(t, err)
	require.False(t, attested)
	require.Equal(t, uint64(2), next)

	// A missed epoch is followed by a rewarded one.
	fake.set(func(fake *fakeBeacon) { fake.head = 41 })
	next, attested, err = attestedFrom(t.Context(), client, chain, validatorIndex, next)
	require.NoError(t, err)
	require.True(t, attested)
	require.Equal(t, uint64(3), next)
	require.Equal(t, 1, fake.requestCount("/qrl/v1/beacon/rewards/attestations/1"))

	// Rewards for another validator are an error, not a miss.
	fake.set(func(fake *fakeBeacon) {
		fake.rewards[3] = strings.Replace(reward(7), `"64"`, `"65"`, 1)
	})
	_, _, err = attestedFrom(t.Context(), client, chain, validatorIndex, 3)
	require.ErrorContains(t, err, "epoch 3 rewards do not cover validator 64")
}

func TestWithdrawalPlanck(t *testing.T) {
	total := withdrawalPlanck([]beacon.Withdrawal{{Amount: 1}, {Amount: 2}})
	require.Zero(t, total.Cmp(new(big.Int).Mul(big.NewInt(3), big.NewInt(params.Shor))), total.String())
	require.Zero(t, withdrawalPlanck(nil).Sign())
}
