package beacon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientDecodesQrysmResponses(t *testing.T) {
	// The handler runs on the server goroutine, so it must use assert rather
	// than require: the test still fails, but FailNow is only valid on the
	// test goroutine.
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/qrl/v1/beacon/genesis":
			_, _ = writer.Write([]byte(`{"data":{"genesis_time":"1700000000","genesis_validators_root":"0x11","genesis_fork_version":"0x20000089"}}`))
		case "/qrl/v1/config/deposit_contract":
			_, _ = writer.Write([]byte(`{"data":{"chain_id":"32382","address":"Q4242424242424242424242424242424242424242"}}`))
		case "/qrl/v1/config/spec":
			_, _ = writer.Write([]byte(`{"data":{"SLOTS_PER_EPOCH":"128","DEPOSIT_CONTRACT_ADDRESS":"Q4242424242424242424242424242424242424242"}}`))
		case "/qrl/v1/beacon/headers/head":
			_, _ = writer.Write([]byte(`{"data":{"root":"0xab","header":{"message":{"slot":"17"}}}}`))
		case "/qrl/v1/beacon/states/head/validators/64":
			_, _ = writer.Write([]byte(`{"execution_optimistic":false,"finalized":false,"data":{"index":"64","balance":"40000000000000","status":"active_ongoing","validator":{"pubkey":"0xab","withdrawal_recipient":"0xcd","effective_balance":"40000000000000","slashed":false,"activation_eligibility_epoch":"3","activation_epoch":"8","exit_epoch":"18446744073709551615","withdrawable_epoch":"18446744073709551615","randao_commitment":"0xef"}}}`))
		case "/qrl/v1/beacon/blocks/9":
			_, _ = writer.Write([]byte(`{"data":{"message":{"slot":"9","body":{"voluntary_exits":[{"message":{"epoch":"1","validator_index":"64"},"signature":"0x00"}],"execution_payload":{"block_number":"42","withdrawals":[{"index":"0","validator_index":"64","address":"0xcd","amount":"40000000000000"}]}}}}}`))
		case "/qrl/v1/validator/duties/attester/2":
			var indices []string
			assert.NoError(t, json.NewDecoder(request.Body).Decode(&indices))
			assert.Equal(t, []string{"64"}, indices)
			_, _ = writer.Write([]byte(`{"dependent_root":"0x00","execution_optimistic":false,"data":[{"pubkey":"0xab","validator_index":"64","committee_index":"0","committee_length":"8","committees_at_slot":"1","validator_committee_index":"3","slot":"17"}]}`))
		case "/qrl/v1/beacon/rewards/attestations/2":
			var indices []string
			assert.NoError(t, json.NewDecoder(request.Body).Decode(&indices))
			assert.Equal(t, []string{"64"}, indices)
			_, _ = writer.Write([]byte(`{"data":{"total_rewards":[{"validator_index":"64","head":"12","target":"34","source":"56"}]}}`))
		case "/qrl/v1/beacon/pool/voluntary_exits":
			assert.Equal(t, http.MethodPost, request.Method)
			assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
			var exit SignedVoluntaryExit
			assert.NoError(t, json.NewDecoder(request.Body).Decode(&exit))
			assert.Equal(t, SignedVoluntaryExit{Message: VoluntaryExit{Epoch: 1, ValidatorIndex: 64}, Signature: "0x00"}, exit)
		case "/qrl/v1/beacon/blocks/broken":
			http.Error(writer, `{"message":"state not available"}`, http.StatusInternalServerError)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := New(server.URL)
	require.NoError(t, err)

	genesis, err := client.Genesis(t.Context())
	require.NoError(t, err)
	require.Equal(t, Genesis{Time: 1700000000, ValidatorsRoot: "0x11", ForkVersion: "0x20000089"}, genesis)

	depositContract, err := client.DepositContract(t.Context())
	require.NoError(t, err)
	require.Equal(t, DepositContract{ChainID: 32382, Address: "Q4242424242424242424242424242424242424242"}, depositContract)

	slotsPerEpoch, err := client.SpecUint(t.Context(), "SLOTS_PER_EPOCH")
	require.NoError(t, err)
	require.Equal(t, uint64(128), slotsPerEpoch)
	_, err = client.SpecUint(t.Context(), "MISSING")
	require.ErrorContains(t, err, "does not define MISSING")
	_, err = client.SpecUint(t.Context(), "DEPOSIT_CONTRACT_ADDRESS")
	require.ErrorContains(t, err, "parse spec value DEPOSIT_CONTRACT_ADDRESS")

	headSlot, err := client.HeadSlot(t.Context())
	require.NoError(t, err)
	require.Equal(t, uint64(17), headSlot)

	validator, err := client.Validator(t.Context(), "64")
	require.NoError(t, err)
	require.Equal(t, Validator{
		Index: 64, Balance: 40000000000000, Status: "active_ongoing", PublicKey: "0xab",
		WithdrawalRecipient: "0xcd", RandaoCommitment: "0xef", EffectiveBalance: 40000000000000,
		ActivationEpoch: 8, ExitEpoch: FarFutureEpoch, WithdrawableEpoch: FarFutureEpoch,
	}, validator)

	operations, err := client.BlockOperations(t.Context(), "9")
	require.NoError(t, err)
	require.Equal(t, BlockOperations{
		Slot:                 9,
		ExecutionBlockNumber: 42,
		VoluntaryExits:       []uint64{64},
		Withdrawals:          []Withdrawal{{ValidatorIndex: 64, Address: "0xcd", Amount: 40000000000000}},
	}, operations)

	duties, err := client.AttesterDuties(t.Context(), 2, []uint64{64})
	require.NoError(t, err)
	require.Equal(t, []AttesterDuty{{PublicKey: "0xab", ValidatorIndex: 64, Slot: 17}}, duties)

	rewards, err := client.AttestationRewards(t.Context(), 2, []uint64{64})
	require.NoError(t, err)
	require.Equal(t, []AttestationReward{{ValidatorIndex: 64, Head: 12, Target: 34, Source: 56}}, rewards)

	require.NoError(t, client.SubmitVoluntaryExit(t.Context(), SignedVoluntaryExit{
		Message: VoluntaryExit{Epoch: 1, ValidatorIndex: 64}, Signature: "0x00",
	}))

	_, err = client.Validator(t.Context(), "65")
	require.True(t, IsNotFound(err), "expected a not-found error, got %v", err)

	_, err = client.BlockOperations(t.Context(), "broken")
	require.EqualError(t, err, `GET /qrl/v1/beacon/blocks/broken returned 500 Internal Server Error: {"message":"state not available"}`)
	require.False(t, IsNotFound(err))
}

func TestNewRejectsRelativeEndpoints(t *testing.T) {
	for _, endpoint := range []string{"", "localhost:3500", "/qrl/v1", "beacon.test"} {
		_, err := New(endpoint)
		require.ErrorContains(t, err, "must be an absolute URL", "endpoint %q", endpoint)
	}
}

type stalledTransport struct{}

func (stalledTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	<-request.Context().Done()
	return nil, request.Context().Err()
}

func TestStalledRequestIsBounded(t *testing.T) {
	for _, test := range []struct {
		name        string
		callerLimit time.Duration
		wantElapsed time.Duration
	}{
		{"request timeout", time.Minute, requestTimeout},
		{"earlier caller deadline", time.Second, time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, err := New("http://beacon.test")
				require.NoError(t, err)
				client.http.Transport = stalledTransport{}
				ctx, cancel := context.WithTimeout(t.Context(), test.callerLimit)
				defer cancel()

				started := time.Now()
				_, err = client.HeadSlot(ctx)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Equal(t, test.wantElapsed, time.Since(started))
			})
		})
	}
}
