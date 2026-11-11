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
		case "/qrl/v1/node/syncing":
			_, _ = writer.Write([]byte(`{"data":{"head_slot":"17","sync_distance":"3","is_syncing":true,"is_optimistic":true,"el_offline":false}}`))
		case "/qrl/v1/node/identity":
			_, _ = writer.Write([]byte(`{"data":{"peer_id":"16Uiu2","qnr":"qnr:-","p2p_addresses":["/ip4/172.16.0.12/tcp/13000/p2p/16Uiu2"],"discovery_addresses":[],"metadata":{"seq_number":"1","attnets":"0x00"}}}`))
		case "/qrl/v1/beacon/headers/head":
			_, _ = writer.Write([]byte(`{"data":{"root":"0xab","header":{"message":{"slot":"17"}}}}`))
		case "/qrl/v1/beacon/states/head/validators/64":
			_, _ = writer.Write([]byte(`{"execution_optimistic":false,"finalized":false,"data":{"index":"64","balance":"40000000000000","status":"active_ongoing","validator":{"pubkey":"0xab","withdrawal_recipient":"0xcd","effective_balance":"40000000000000","slashed":false,"activation_eligibility_epoch":"3","activation_epoch":"8","exit_epoch":"18446744073709551615","withdrawable_epoch":"18446744073709551615","randao_commitment":"0xef"}}}`))
		case "/qrl/v1/beacon/states/95/validators/64":
			_, _ = writer.Write([]byte(`{"data":{"index":"64","balance":"39998000000000","status":"active_ongoing","validator":{"pubkey":"0xab","effective_balance":"39998000000000","exit_epoch":"18446744073709551615","withdrawable_epoch":"18446744073709551615"}}}`))
		case "/qrl/v1/beacon/states/95/validators":
			assert.Equal(t, []string{"active_ongoing", "active_exiting", "active_slashed"}, request.URL.Query()["status"])
			_, _ = writer.Write([]byte(`{"data":[{"index":"0","validator":{"effective_balance":"40000000000000"}},{"index":"64","validator":{"effective_balance":"39998000000000"}}]}`))
		case "/qrl/v1/beacon/blocks/9":
			_, _ = writer.Write([]byte(`{"data":{"message":{"slot":"9","proposer_index":"12","body":{"voluntary_exits":[{"message":{"epoch":"1","validator_index":"64"},"signature":"0x00"}],"execution_payload":{"block_number":"42","withdrawals":[{"index":"0","validator_index":"64","address":"0xcd","amount":"40000000000000"}]}}}}}`))
		case "/qrl/v1/beacon/blocks/21":
			_, _ = writer.Write([]byte(`{"data":{"message":{"slot":"21","proposer_index":"3","body":{` +
				`"proposer_slashings":[{"signed_header_1":{"message":{"slot":"19","proposer_index":"64","parent_root":"0x01","state_root":"0x02","body_root":"0x03"},"signature":"0x00"},` +
				`"signed_header_2":{"message":{"slot":"19","proposer_index":"64","parent_root":"0x01","state_root":"0x04","body_root":"0x05"},"signature":"0x00"}}],` +
				`"attester_slashings":[{"attestation_1":{"attesting_indices":["7","64"],"data":{"slot":"18","index":"0","beacon_block_root":"0xaa","source":{"epoch":"1","root":"0x0a"},"target":{"epoch":"2","root":"0x0b"}},"signatures":["0x00","0x00"]},` +
				`"attestation_2":{"attesting_indices":["64"],"data":{"slot":"18","index":"0","beacon_block_root":"0xbb","source":{"epoch":"1","root":"0x0a"},"target":{"epoch":"2","root":"0x0b"}},"signatures":["0x00"]}}],` +
				`"execution_payload":{"block_number":"50","withdrawals":[]}}}}}`))
		case "/qrl/v1/beacon/rewards/blocks/21":
			_, _ = writer.Write([]byte(`{"data":{"proposer_index":"3","total":"78125000100","attestations":"100","sync_aggregate":"0","proposer_slashings":"0","attester_slashings":"78125000000"}}`))
		case "/qrl/v1/beacon/blocks/22":
			_, _ = writer.Write([]byte(`{"data":{"message":{"slot":"22","body":{"attester_slashings":[{"attestation_1":{"attesting_indices":["x"]},"attestation_2":{}}]}}}}`))
		case "/qrl/v1/validator/duties/proposer/2":
			assert.Equal(t, http.MethodGet, request.Method)
			_, _ = writer.Write([]byte(`{"dependent_root":"0x00","execution_optimistic":false,"data":[{"pubkey":"0xab","validator_index":"64","slot":"16"},{"pubkey":"0xcd","validator_index":"3","slot":"17"}]}`))
		case "/qrl/v1/validator/duties/attester/2":
			assertIndices(t, request, "64")
			_, _ = writer.Write([]byte(`{"dependent_root":"0x00","execution_optimistic":false,"data":[{"pubkey":"0xab","validator_index":"64","committee_index":"0","committee_length":"8","committees_at_slot":"1","validator_committee_index":"3","slot":"17"}]}`))
		case "/qrl/v1/beacon/rewards/attestations/2":
			assertIndices(t, request, "64")
			_, _ = writer.Write([]byte(`{"data":{"total_rewards":[{"validator_index":"64","head":"12","target":"34","source":"56"}]}}`))
		case "/qrl/v1/beacon/rewards/attestations/4":
			assertIndices(t, request, "64")
			_, _ = writer.Write([]byte(`{"data":{"ideal_rewards":[{"effective_balance":"39998000000000","head":"376","target":"698","source":"376"}],"total_rewards":[]}}`))
		case "/qrl/v1/beacon/rewards/blocks/9":
			_, _ = writer.Write([]byte(`{"data":{"proposer_index":"12","total":"123","attestations":"100","sync_aggregate":"23","proposer_slashings":"0","attester_slashings":"0"}}`))
		case "/qrl/v1/beacon/rewards/attestations/3":
			assertIndices(t, request, "64")
			_, _ = writer.Write([]byte(`{"data":{"total_rewards":[{"validator_index":"64","head":"0","target":"-650","source":"-350"}]}}`))
		case "/qrl/v1/beacon/rewards/sync_committee/9":
			assertIndices(t, request, "64")
			_, _ = writer.Write([]byte(`{"execution_optimistic":false,"finalized":false,"data":[{"validator_index":"64","reward":"-2964"}]}`))
		case "/qrl/v1/beacon/pool/voluntary_exits":
			assert.Equal(t, http.MethodPost, request.Method)
			assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
			var exit SignedVoluntaryExit
			assert.NoError(t, json.NewDecoder(request.Body).Decode(&exit))
			assert.Equal(t, SignedVoluntaryExit{Message: VoluntaryExit{Epoch: 1, ValidatorIndex: 64}, Signature: "0x00"}, exit)
		case "/qrl/v1/beacon/pool/proposer_slashings":
			assert.Equal(t, http.MethodPost, request.Method)
			var body map[string]any
			assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			assert.Equal(t, map[string]any{
				"signed_header_1": map[string]any{
					"message": map[string]any{
						"slot": "19", "proposer_index": "64", "parent_root": "0x01", "state_root": "0x02", "body_root": "0x03",
					},
					"signature": "0xaa",
				},
				"signed_header_2": map[string]any{
					"message": map[string]any{
						"slot": "19", "proposer_index": "64", "parent_root": "0x01", "state_root": "0x02", "body_root": "0x04",
					},
					"signature": "0xbb",
				},
			}, body)
		case "/qrl/v1/beacon/pool/attestations":
			assert.Equal(t, http.MethodPost, request.Method)
			var body []map[string]any
			assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			assert.Equal(t, []map[string]any{{
				"aggregation_bits": "0x0c",
				"data": map[string]any{
					"slot": "18", "index": "1", "beacon_block_root": "0xaa",
					"source": map[string]any{"epoch": "1", "root": "0x0a"},
					"target": map[string]any{"epoch": "2", "root": "0x0b"},
				},
				"signatures": []any{"0xcc"},
			}}, body)
		case "/qrl/v1/beacon/pool/attester_slashings":
			assert.Equal(t, http.MethodPost, request.Method)
			var body map[string]any
			assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			vote := func(blockRoot, signature string) map[string]any {
				return map[string]any{
					"attesting_indices": []any{"64"},
					"data": map[string]any{
						"slot": "18", "index": "0", "beacon_block_root": blockRoot,
						"source": map[string]any{"epoch": "1", "root": "0x0a"},
						"target": map[string]any{"epoch": "2", "root": "0x0b"},
					},
					"signatures": []any{signature},
				}
			}
			assert.Equal(t, map[string]any{"attestation_1": vote("0xaa", "0xcc"), "attestation_2": vote("0xbb", "0xdd")}, body)
		case "/qrl/v1/beacon/headers/16":
			_, _ = writer.Write([]byte(`{"data":{"root":"0x16","header":{"message":{"slot":"16"}}}}`))
		case "/qrl/v1/beacon/states/head/fork":
			_, _ = writer.Write([]byte(`{"execution_optimistic":false,"finalized":false,"data":{"previous_version":"0x10000020","current_version":"0x10000021","epoch":"5"}}`))
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

	syncStatus, err := client.SyncStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, SyncStatus{HeadSlot: 17, SyncDistance: 3, IsSyncing: true, IsOptimistic: true}, syncStatus)

	identity, err := client.Identity(t.Context())
	require.NoError(t, err)
	require.Equal(t, Identity{PeerID: "16Uiu2", P2PAddresses: []string{"/ip4/172.16.0.12/tcp/13000/p2p/16Uiu2"}}, identity)

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

	validator, err = client.ValidatorAt(t.Context(), "95", "64")
	require.NoError(t, err)
	require.Equal(t, Validator{
		Index: 64, Balance: 39998000000000, Status: "active_ongoing", PublicKey: "0xab",
		EffectiveBalance: 39998000000000, ExitEpoch: FarFutureEpoch, WithdrawableEpoch: FarFutureEpoch,
	}, validator)

	totalActive, err := client.TotalActiveBalance(t.Context(), "95")
	require.NoError(t, err)
	require.Equal(t, uint64(79998000000000), totalActive)

	operations, err := client.BlockOperations(t.Context(), "9")
	require.NoError(t, err)
	require.Equal(t, BlockOperations{
		Slot:                 9,
		ProposerIndex:        12,
		ExecutionBlockNumber: 42,
		VoluntaryExits:       []uint64{64},
		Withdrawals:          []Withdrawal{{ValidatorIndex: 64, Address: "0xcd", Amount: 40000000000000}},
	}, operations)

	operations, err = client.BlockOperations(t.Context(), "21")
	require.NoError(t, err)
	require.Equal(t, []ProposerSlashing{{
		Header1: BeaconBlockHeaderMessage{Slot: 19, ProposerIndex: 64, ParentRoot: "0x01", StateRoot: "0x02", BodyRoot: "0x03"},
		Header2: BeaconBlockHeaderMessage{Slot: 19, ProposerIndex: 64, ParentRoot: "0x01", StateRoot: "0x04", BodyRoot: "0x05"},
	}}, operations.ProposerSlashings)
	vote := func(blockRoot string) AttestationData {
		return AttestationData{
			Slot: 18, BeaconBlockRoot: blockRoot,
			Source: Checkpoint{Epoch: 1, Root: "0x0a"}, Target: Checkpoint{Epoch: 2, Root: "0x0b"},
		}
	}
	require.Equal(t, []AttesterSlashing{{
		Attestation1: IndexedAttestation{AttestingIndices: []uint64{7, 64}, Data: vote("0xaa")},
		Attestation2: IndexedAttestation{AttestingIndices: []uint64{64}, Data: vote("0xbb")},
	}}, operations.AttesterSlashings)

	_, err = client.BlockOperations(t.Context(), "22")
	require.ErrorContains(t, err, `attesting index "x"`)

	proposers, err := client.ProposerDuties(t.Context(), 2)
	require.NoError(t, err)
	require.Equal(t, []ProposerDuty{{PublicKey: "0xab", ValidatorIndex: 64, Slot: 16}, {PublicKey: "0xcd", ValidatorIndex: 3, Slot: 17}}, proposers)

	duties, err := client.AttesterDuties(t.Context(), 2, []uint64{64})
	require.NoError(t, err)
	require.Equal(t, []AttesterDuty{{
		PublicKey: "0xab", ValidatorIndex: 64, Slot: 17, CommitteeLength: 8, ValidatorCommitteeIndex: 3,
	}}, duties)

	rewards, err := client.AttestationRewards(t.Context(), 2, []uint64{64})
	require.NoError(t, err)
	require.Equal(t, []AttestationReward{{ValidatorIndex: 64, Head: 12, Target: 34, Source: 56}}, rewards)

	// A missed source or target vote is reported as a negative score.
	rewards, err = client.AttestationRewards(t.Context(), 3, []uint64{64})
	require.NoError(t, err)
	require.Equal(t, []AttestationReward{{ValidatorIndex: 64, Target: -650, Source: -350}}, rewards)

	ideal, err := client.IdealAttestationRewards(t.Context(), 4, []uint64{64})
	require.NoError(t, err)
	require.Equal(t, []IdealAttestationReward{{EffectiveBalance: 39998000000000, Head: 376, Target: 698, Source: 376}}, ideal)

	blockRewards, err := client.BlockRewards(t.Context(), "9")
	require.NoError(t, err)
	require.Equal(t, BlockRewards{Total: 123}, blockRewards)
	blockRewards, err = client.BlockRewards(t.Context(), "21")
	require.NoError(t, err)
	require.Equal(t, BlockRewards{Total: 78125000100, AttesterSlashings: 78125000000}, blockRewards)

	syncRewards, err := client.SyncCommitteeRewards(t.Context(), "9", []uint64{64})
	require.NoError(t, err)
	require.Equal(t, []SyncCommitteeReward{{ValidatorIndex: 64, Reward: -2964}}, syncRewards)

	require.NoError(t, client.SubmitVoluntaryExit(t.Context(), SignedVoluntaryExit{
		Message: VoluntaryExit{Epoch: 1, ValidatorIndex: 64}, Signature: "0x00",
	}))

	header := BeaconBlockHeaderMessage{Slot: 19, ProposerIndex: 64, ParentRoot: "0x01", StateRoot: "0x02", BodyRoot: "0x03"}
	other := header
	other.BodyRoot = "0x04"
	require.NoError(t, client.SubmitProposerSlashing(t.Context(), SignedProposerSlashing{
		SignedHeader1: SignedBeaconBlockHeader{Message: header, Signature: "0xaa"},
		SignedHeader2: SignedBeaconBlockHeader{Message: other, Signature: "0xbb"},
	}))

	require.NoError(t, client.SubmitAttestations(t.Context(), []SignedAttestation{{
		AggregationBits: "0x0c",
		Data: AttestationData{
			Slot: 18, CommitteeIndex: 1, BeaconBlockRoot: "0xaa",
			Source: Checkpoint{Epoch: 1, Root: "0x0a"}, Target: Checkpoint{Epoch: 2, Root: "0x0b"},
		},
		Signatures: []string{"0xcc"},
	}}))

	signedVote := func(blockRoot, signature string) SignedIndexedAttestation {
		return SignedIndexedAttestation{
			AttestingIndices: []string{"64"},
			Data: AttestationData{
				Slot: 18, BeaconBlockRoot: blockRoot,
				Source: Checkpoint{Epoch: 1, Root: "0x0a"}, Target: Checkpoint{Epoch: 2, Root: "0x0b"},
			},
			Signatures: []string{signature},
		}
	}
	require.NoError(t, client.SubmitAttesterSlashing(t.Context(), SignedAttesterSlashing{
		Attestation1: signedVote("0xaa", "0xcc"), Attestation2: signedVote("0xbb", "0xdd"),
	}))

	// Slots 17 to 19 have no block in this server, so the lookup steps back to 16.
	root, err := client.RootAtOrBefore(t.Context(), 19)
	require.NoError(t, err)
	require.Equal(t, "0x16", root)

	fork, err := client.Fork(t.Context(), "head")
	require.NoError(t, err)
	require.Equal(t, Fork{PreviousVersion: "0x10000020", CurrentVersion: "0x10000021", Epoch: 5}, fork)

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

func TestClientDecodesChainProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/qrl/v1/beacon/headers/32":
			_, _ = writer.Write([]byte(`{"data":{"root":"0xab","canonical":true,"header":{"message":{"slot":"32"}}}}`))
		case "/qrl/v1/beacon/states/head/finality_checkpoints":
			_, _ = writer.Write([]byte(`{"data":{"previous_justified":{"epoch":"2","root":"0x02"},"current_justified":{"epoch":"3","root":"0x03"},"finalized":{"epoch":"2","root":"0x02"}}}`))
		case "/qrl/v1/beacon/states/95/finality_checkpoints":
			_, _ = writer.Write([]byte(`{"data":{"previous_justified":{"epoch":"9","root":"0x09"},"current_justified":{"epoch":"10","root":"0x0a"},"finalized":{"epoch":"9","root":"0x09"}}}`))
		case "/qrl/v1/beacon/states/head/validators":
			assert.Equal(t, []string{"active_slashed", "exited_slashed"}, request.URL.Query()["status"])
			_, _ = writer.Write([]byte(`{"data":[{"index":"7","balance":"1","status":"active_slashed","validator":{"pubkey":"0xab","slashed":true,"effective_balance":"1","activation_epoch":"0","exit_epoch":"9","withdrawable_epoch":"73"}}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := New(server.URL)
	require.NoError(t, err)

	header, err := client.BlockHeader(t.Context(), "32")
	require.NoError(t, err)
	require.Equal(t, BlockHeader{Slot: 32, Root: "0xab"}, header)

	_, err = client.BlockHeader(t.Context(), "33")
	require.True(t, IsNotFound(err))

	checkpoints, err := client.FinalityCheckpoints(t.Context())
	require.NoError(t, err)
	require.Equal(t, FinalityCheckpoints{
		PreviousJustified: Checkpoint{Epoch: 2, Root: "0x02"},
		CurrentJustified:  Checkpoint{Epoch: 3, Root: "0x03"},
		Finalized:         Checkpoint{Epoch: 2, Root: "0x02"},
	}, checkpoints)

	checkpoints, err = client.FinalityCheckpointsAt(t.Context(), "95")
	require.NoError(t, err)
	require.Equal(t, FinalityCheckpoints{
		PreviousJustified: Checkpoint{Epoch: 9, Root: "0x09"},
		CurrentJustified:  Checkpoint{Epoch: 10, Root: "0x0a"},
		Finalized:         Checkpoint{Epoch: 9, Root: "0x09"},
	}, checkpoints)

	slashed, err := client.SlashedValidators(t.Context())
	require.NoError(t, err)
	require.Equal(t, []Validator{{
		Index: 7, Balance: 1, Status: "active_slashed", PublicKey: "0xab", Slashed: true,
		EffectiveBalance: 1, ExitEpoch: 9, WithdrawableEpoch: 73,
	}}, slashed)
}

// assertIndices checks that a request POSTs the validator indices as a JSON
// array of decimal strings.
func assertIndices(t *testing.T, request *http.Request, want ...string) {
	t.Helper()
	assert.Equal(t, http.MethodPost, request.Method)
	var indices []string
	assert.NoError(t, json.NewDecoder(request.Body).Decode(&indices))
	assert.Equal(t, want, indices)
}

func TestAttestationIncludes(t *testing.T) {
	// A committee of three in which members 0 and 2 voted: bits 0 and 2, and
	// bit 3 marking the length.
	attestation := Attestation{AggregationBits: []byte{0b1101}}
	require.True(t, attestation.Includes(0))
	require.False(t, attestation.Includes(1))
	require.True(t, attestation.Includes(2))
	require.False(t, attestation.Includes(3), "the length marker is no vote")
	require.False(t, attestation.Includes(9))

	// A committee of nine: member 8 is in the second byte, before the marker.
	require.True(t, Attestation{AggregationBits: []byte{0x00, 0b11}}.Includes(8))
	require.False(t, Attestation{}.Includes(0))
}

func TestSingleMemberBits(t *testing.T) {
	require.Equal(t, []byte{0b1100}, SingleMemberBits(3, 2))
	// A committee of eight needs a second byte for the length marker.
	require.Equal(t, []byte{0b10, 0b1}, SingleMemberBits(8, 1))

	bits := SingleMemberBits(13, 9)
	require.Equal(t, uint64(13), bitlistLen(bits))
	for position := range uint64(13) {
		require.Equal(t, position == 9, Attestation{AggregationBits: bits}.Includes(position), "member %d", position)
	}
}

func TestClientDecodesAttestationsAndCommittees(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/qrl/v1/beacon/blocks/44":
			_, _ = writer.Write([]byte(`{"data":{"message":{"slot":"44","proposer_index":"3","body":{"attestations":[` +
				`{"aggregation_bits":"0x0d","data":{"slot":"43","index":"1","beacon_block_root":"0xaa",` +
				`"source":{"epoch":"4","root":"0x0a"},"target":{"epoch":"5","root":"0x0b"}},"signatures":["0x00","0x00"]}],` +
				`"execution_payload":{"block_number":"7","withdrawals":[]}}}}}`))
		case "/qrl/v1/beacon/blocks/45":
			_, _ = writer.Write([]byte(`{"data":{"message":{"body":{"attestations":[{"aggregation_bits":"0xzz"}]}}}}`))
		case "/qrl/v1/beacon/states/40/committees":
			assert.Equal(t, "5", request.URL.Query().Get("epoch"))
			_, _ = writer.Write([]byte(`{"data":[{"index":"1","slot":"43","validators":["7","64","9"]}]}`))
		case "/qrl/v1/beacon/states/41/committees":
			_, _ = writer.Write([]byte(`{"data":[{"index":"0","slot":"41","validators":["x"]}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := New(server.URL)
	require.NoError(t, err)

	operations, err := client.BlockOperations(t.Context(), "44")
	require.NoError(t, err)
	require.Equal(t, []Attestation{{
		AggregationBits: []byte{0x0d},
		Data: AttestationData{
			Slot: 43, CommitteeIndex: 1, BeaconBlockRoot: "0xaa",
			Source: Checkpoint{Epoch: 4, Root: "0x0a"}, Target: Checkpoint{Epoch: 5, Root: "0x0b"},
		},
	}}, operations.Attestations)

	_, err = client.BlockOperations(t.Context(), "45")
	require.ErrorContains(t, err, `aggregation bits "0xzz"`)

	committees, err := client.Committees(t.Context(), "40", 5)
	require.NoError(t, err)
	require.Equal(t, []Committee{{Index: 1, Slot: 43, Validators: []uint64{7, 64, 9}}}, committees)

	_, err = client.Committees(t.Context(), "41", 5)
	require.ErrorContains(t, err, `committee validator "x"`)
}

func TestSpecUints(t *testing.T) {
	spec := Spec{"SLOTS_PER_EPOCH": "8", "SECONDS_PER_SLOT": "5", "DEPOSIT_CONTRACT_ADDRESS": "0x01"}

	var slots, seconds uint64
	require.NoError(t, spec.Uints(map[string]*uint64{"SLOTS_PER_EPOCH": &slots, "SECONDS_PER_SLOT": &seconds}))
	require.Equal(t, uint64(8), slots)
	require.Equal(t, uint64(5), seconds)

	// Names are read in order, so the first failing one is always reported.
	err := spec.Uints(map[string]*uint64{"MISSING": new(uint64), "DEPOSIT_CONTRACT_ADDRESS": new(uint64)})
	require.ErrorContains(t, err, "parse spec value DEPOSIT_CONTRACT_ADDRESS")

	require.ErrorContains(t, spec.Uints(map[string]*uint64{"MISSING": new(uint64)}), "does not define MISSING")
}
