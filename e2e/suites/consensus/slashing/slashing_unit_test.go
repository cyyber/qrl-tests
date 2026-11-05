package slashing

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/stretchr/testify/require"
)

const shorPerQRL = 1_000_000_000

func testSlashingSpec() slashingSpec {
	return slashingSpec{
		minSlashingPenaltyQuotient:  32,
		whistleblowerRewardQuotient: 512,
		epochsPerSlashingsVector:    512,
		maxSeedLookahead:            4,
		minWithdrawabilityDelay:     2,
	}
}

func TestLoadSlashingSpec(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = fmt.Fprint(writer, `{"data":{
			"MIN_SLASHING_PENALTY_QUOTIENT":"32","WHISTLEBLOWER_REWARD_QUOTIENT":"512",
			"EPOCHS_PER_SLASHINGS_VECTOR":"512","MAX_SEED_LOOKAHEAD":"4",
			"MIN_VALIDATOR_WITHDRAWABILITY_DELAY":"2"}}`)
	}))
	t.Cleanup(server.Close)
	client, err := beacon.New(server.URL)
	require.NoError(t, err)

	spec, err := loadSlashingSpec(t.Context(), client)
	require.NoError(t, err)
	require.Equal(t, testSlashingSpec(), spec)
}

func TestSlashingAmounts(t *testing.T) {
	spec := testSlashingSpec()
	require.Equal(t, uint64(1_250*shorPerQRL), spec.initialPenalty(40_000*shorPerQRL))
	require.Equal(t, uint64(78_125_000_000), spec.whistleblowerReward(40_000*shorPerQRL))
}

func TestSlashingEpochs(t *testing.T) {
	spec := testSlashingSpec()
	exit := spec.exitEpoch(10)
	require.Equal(t, uint64(15), exit)
	// The slashings vector outlasts the withdrawability delay.
	require.Equal(t, uint64(522), spec.withdrawableEpoch(10, exit))

	spec.minWithdrawabilityDelay = 600
	require.Equal(t, uint64(615), spec.withdrawableEpoch(10, exit))
}

func vote(slot, source, target uint64, blockRoot string) beacon.AttestationData {
	return beacon.AttestationData{
		Slot:            slot,
		BeaconBlockRoot: blockRoot,
		Source:          beacon.Checkpoint{Epoch: source, Root: "0x0a"},
		Target:          beacon.Checkpoint{Epoch: target, Root: "0x0b"},
	}
}

func TestConflictingVotes(t *testing.T) {
	// Two heads for one slot, as two nodes with different views vote.
	require.True(t, isDoubleVote(vote(18, 1, 2, "0xaa"), vote(18, 1, 2, "0xbb")))
	require.False(t, isSurroundVote(vote(18, 1, 2, "0xaa"), vote(18, 1, 2, "0xbb")))
	// The same vote signed twice is no offense.
	require.False(t, isDoubleVote(vote(18, 1, 2, "0xaa"), vote(18, 1, 2, "0xaa")))
	// Votes for different targets are a double vote only if they surround.
	require.False(t, isDoubleVote(vote(18, 1, 2, "0xaa"), vote(26, 2, 3, "0xbb")))
	require.False(t, isSurroundVote(vote(18, 1, 2, "0xaa"), vote(26, 2, 3, "0xbb")))
	require.True(t, isSurroundVote(vote(18, 1, 4, "0xaa"), vote(26, 2, 3, "0xbb")))
	require.True(t, isSurroundVote(vote(26, 2, 3, "0xbb"), vote(18, 1, 4, "0xaa")))
}

func TestEvidenceAgainst(t *testing.T) {
	attester := beacon.AttesterSlashing{
		Attestation1: beacon.IndexedAttestation{AttestingIndices: []uint64{7, 64}, Data: vote(18, 1, 2, "0xaa")},
		Attestation2: beacon.IndexedAttestation{AttestingIndices: []uint64{64}, Data: vote(18, 1, 2, "0xbb")},
	}
	proposer := beacon.ProposerSlashing{
		Header1: beacon.BeaconBlockHeaderMessage{Slot: 19, ProposerIndex: 5, BodyRoot: "0x01"},
		Header2: beacon.BeaconBlockHeaderMessage{Slot: 19, ProposerIndex: 5, BodyRoot: "0x02"},
	}
	operations := beacon.BlockOperations{
		AttesterSlashings: []beacon.AttesterSlashing{attester},
		ProposerSlashings: []beacon.ProposerSlashing{proposer},
	}

	evidence, found := evidenceAgainst(operations, 64)
	require.True(t, found)
	require.Equal(t, &attester, evidence.attester)
	require.Nil(t, evidence.proposer)

	// Validator 7 signed only one of the attestations, so it is not slashed.
	_, found = evidenceAgainst(operations, 7)
	require.False(t, found)

	evidence, found = evidenceAgainst(operations, 5)
	require.True(t, found)
	require.Equal(t, &proposer, evidence.proposer)
	require.Nil(t, evidence.attester)

	_, found = evidenceAgainst(beacon.BlockOperations{}, 64)
	require.False(t, found)
}
