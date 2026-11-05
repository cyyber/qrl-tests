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

func TestDoubleVote(t *testing.T) {
	// Two heads for one slot, as two nodes with different views vote.
	require.True(t, isDoubleVote(vote(18, 1, 2, "0xaa"), vote(18, 1, 2, "0xbb")))
	// The same vote signed twice is no offense.
	require.False(t, isDoubleVote(vote(18, 1, 2, "0xaa"), vote(18, 1, 2, "0xaa")))
	// Votes for different targets are no double vote.
	require.False(t, isDoubleVote(vote(18, 1, 2, "0xaa"), vote(26, 2, 3, "0xbb")))
}

func TestProposalCandidates(t *testing.T) {
	duty := func(slot, index uint64) beacon.ProposerDuty {
		return beacon.ProposerDuty{Slot: slot, ValidatorIndex: index}
	}
	current := []beacon.ProposerDuty{duty(16, 3), duty(17, 40), duty(18, 9)}
	next := []beacon.ProposerDuty{duty(24, 1), duty(25, 9), duty(26, 33), duty(27, 2), duty(28, 1), duty(29, 5)}
	// Node 1 runs 0 to 31; validator 5 is already slashed.
	candidates := []uint64{1, 2, 3, 5, 9}

	// 1 proposes twice and 9 proposed in the current epoch, so another
	// proposal of theirs would be a second offense; 33 is not node 1's.
	require.Equal(t, []beacon.ProposerDuty{duty(27, 2)}, proposalCandidates(current, next, candidates, []uint64{5}))
	require.Equal(t, []beacon.ProposerDuty{duty(29, 5), duty(27, 2)}, proposalCandidates(current, next, candidates, nil))
	require.Empty(t, proposalCandidates(current, nil, candidates, nil))
}

func TestDoubleProposal(t *testing.T) {
	header := func(slot, proposer uint64, body string) beacon.BeaconBlockHeaderMessage {
		return beacon.BeaconBlockHeaderMessage{Slot: slot, ProposerIndex: proposer, ParentRoot: "0x10", BodyRoot: body}
	}
	// Two blocks for one slot, as two clients with different graffiti propose.
	require.True(t, isDoubleProposal(header(19, 5, "0x01"), header(19, 5, "0x02")))
	// The same block signed twice is no offense.
	require.False(t, isDoubleProposal(header(19, 5, "0x01"), header(19, 5, "0x01")))
	// Blocks of different slots or proposers are not one proposer's two blocks.
	require.False(t, isDoubleProposal(header(19, 5, "0x01"), header(20, 5, "0x02")))
	require.False(t, isDoubleProposal(header(19, 5, "0x01"), header(19, 6, "0x02")))
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
