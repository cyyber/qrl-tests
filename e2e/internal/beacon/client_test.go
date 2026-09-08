package beacon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatorDecodesQrysmRecord(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/qrl/v1/beacon/states/head/validators/64":
			_, _ = writer.Write([]byte(`{"execution_optimistic":false,"finalized":false,"data":{"index":"64","balance":"40000000000000","status":"active_ongoing","validator":{"pubkey":"0xab","withdrawal_recipient":"0xcd","effective_balance":"40000000000000","slashed":false,"activation_eligibility_epoch":"3","activation_epoch":"8","exit_epoch":"18446744073709551615","withdrawable_epoch":"18446744073709551615","randao_commitment":"0xef"}}}`))
		case "/qrl/v1/beacon/blocks/9":
			_, _ = writer.Write([]byte(`{"data":{"message":{"slot":"9","body":{"voluntary_exits":[{"message":{"epoch":"1","validator_index":"64"},"signature":"0x00"}],"execution_payload":{"withdrawals":[{"index":"0","validator_index":"64","address":"0xcd","amount":"40000000000000"}]}}}}}`))
		case "/qrl/v1/validator/duties/attester/2":
			var indices []string
			require.NoError(t, json.NewDecoder(request.Body).Decode(&indices))
			require.Equal(t, []string{"64"}, indices)
			_, _ = writer.Write([]byte(`{"dependent_root":"0x00","execution_optimistic":false,"data":[{"pubkey":"0xab","validator_index":"64","committee_index":"0","committee_length":"8","committees_at_slot":"1","validator_committee_index":"3","slot":"17"}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := New(server.URL)
	require.NoError(t, err)

	validator, err := client.Validator(context.Background(), "64")
	require.NoError(t, err)
	require.Equal(t, Validator{
		Index: 64, Balance: 40000000000000, Status: "active_ongoing", PublicKey: "0xab",
		WithdrawalRecipient: "0xcd", RandaoCommitment: "0xef", EffectiveBalance: 40000000000000,
		ActivationEpoch: 8, ExitEpoch: FarFutureEpoch, WithdrawableEpoch: FarFutureEpoch,
	}, validator)

	operations, err := client.BlockOperations(context.Background(), "9")
	require.NoError(t, err)
	require.Equal(t, uint64(9), operations.Slot)
	require.Equal(t, []uint64{64}, operations.VoluntaryExits)
	require.Equal(t, []Withdrawal{{ValidatorIndex: 64, Address: "0xcd", Amount: 40000000000000}}, operations.Withdrawals)

	duties, err := client.AttesterDuties(context.Background(), 2, []uint64{64})
	require.NoError(t, err)
	require.Equal(t, []AttesterDuty{{PublicKey: "0xab", ValidatorIndex: 64, Slot: 17}}, duties)

	_, err = client.Validator(context.Background(), "65")
	require.True(t, IsNotFound(err), "expected a not-found error, got %v", err)
}
