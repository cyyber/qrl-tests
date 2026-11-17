package beacon

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttestationVoters(t *testing.T) {
	require.Equal(t, 2, Attestation{AggregationBits: []byte{0b1101}}.Voters())
	// A full committee of 32 and its length bit in a fifth byte.
	require.Equal(t, 32, Attestation{AggregationBits: []byte{0xff, 0xff, 0xff, 0xff, 0x01}}.Voters())
	require.Zero(t, Attestation{}.Voters())
}

func TestClientDecodesSyncAggregatesAndExecutionData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/qrl/v1/beacon/blocks/30":
			_, _ = writer.Write([]byte(`{"data":{"message":{"slot":"30","proposer_index":"5","body":{` +
				`"execution_data":{"deposit_root":"0xd0","deposit_count":"304","block_hash":"0xb0"},` +
				`"deposits":[{"proof":[],"data":{}},{"proof":[],"data":{}}],` +
				`"sync_aggregate":{"sync_committee_bits":"0xff0f","sync_committee_signatures":["0x00"]},` +
				`"execution_payload":{"block_number":"29","gas_used":"29000000","gas_limit":"30000000",` +
				`"withdrawals":[{"index":"77","validator_index":"6","address":"0xcd","amount":"5"}]}}}}}`))
		case "/qrl/v1/beacon/blocks/31":
			_, _ = writer.Write([]byte(`{"data":{"message":{"body":{"sync_aggregate":{"sync_committee_bits":"0xzz"}}}}}`))
		case "/qrl/v1/beacon/states/head/sync_committees":
			assert.Equal(t, "3", request.URL.Query().Get("epoch"))
			_, _ = writer.Write([]byte(`{"execution_optimistic":false,"finalized":false,"data":{"validators":["4","9","4"],"validator_aggregates":[["4","9","4"]]}}`))
		case "/qrl/v1/beacon/states/30/sync_committees":
			_, _ = writer.Write([]byte(`{"data":{"validators":["x"]}}`))
		case "/qrl/v1/beacon/pool/attester_slashings":
			assert.Equal(t, http.MethodPost, request.Method)
			body, err := io.ReadAll(request.Body)
			assert.NoError(t, err)
			assert.JSONEq(t, `{
				"attestation_1":{"attesting_indices":["3","7"],"data":{"slot":"16","index":"0","beacon_block_root":"0x01",
					"source":{"epoch":"0","root":"0x00"},"target":{"epoch":"2","root":"0x00"}},"signatures":["0xaa","0xbb"]},
				"attestation_2":{"attesting_indices":["7"],"data":{"slot":"16","index":"0","beacon_block_root":"0x02",
					"source":{"epoch":"0","root":"0x00"},"target":{"epoch":"2","root":"0x00"}},"signatures":["0xcc"]}
			}`, string(body))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := New(server.URL)
	require.NoError(t, err)

	operations, err := client.BlockOperations(t.Context(), "30")
	require.NoError(t, err)
	require.Equal(t, BlockOperations{
		Slot:                 30,
		ProposerIndex:        5,
		ExecutionBlockNumber: 29,
		ExecutionGasUsed:     29000000,
		ExecutionGasLimit:    30000000,
		ExecutionData:        ExecutionData{DepositRoot: "0xd0", DepositCount: 304, BlockHash: "0xb0"},
		SyncCommitteeBits:    []byte{0xff, 0x0f},
		Deposits:             2,
		Withdrawals:          []Withdrawal{{Index: 77, ValidatorIndex: 6, Address: "0xcd", Amount: 5}},
	}, operations)
	require.Equal(t, 12, operations.SyncParticipants())

	_, err = client.BlockOperations(t.Context(), "31")
	require.ErrorContains(t, err, `sync committee bits "0xzz"`)

	committee, err := client.SyncCommittee(t.Context(), "head", 3)
	require.NoError(t, err)
	require.Equal(t, []uint64{4, 9, 4}, committee)
	_, err = client.SyncCommittee(t.Context(), "30", 3)
	require.ErrorContains(t, err, `sync committee validator "x"`)

	vote := func(head string) AttestationData {
		return AttestationData{Slot: 16, BeaconBlockRoot: head, Source: Checkpoint{Root: "0x00"}, Target: Checkpoint{Epoch: 2, Root: "0x00"}}
	}
	require.NoError(t, client.SubmitAttesterSlashing(t.Context(), SignedAttesterSlashing{
		Attestation1: SignedIndexedAttestation{AttestingIndices: []uint64{3, 7}, Data: vote("0x01"), Signatures: []string{"0xaa", "0xbb"}},
		Attestation2: SignedIndexedAttestation{AttestingIndices: []uint64{7}, Data: vote("0x02"), Signatures: []string{"0xcc"}},
	}))
}

func TestSignedIndexedAttestationJSON(t *testing.T) {
	encoded, err := json.Marshal(SignedIndexedAttestation{AttestingIndices: []uint64{1}, Signatures: []string{"0x00"}})
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"attesting_indices":["1"]`)
}
