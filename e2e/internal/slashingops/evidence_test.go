package slashingops

import (
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/stretchr/testify/require"
)

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
	require.True(t, IsDoubleVote(vote(18, 1, 2, "0xaa"), vote(18, 1, 2, "0xbb")))
	// The same vote signed twice is no offense.
	require.False(t, IsDoubleVote(vote(18, 1, 2, "0xaa"), vote(18, 1, 2, "0xaa")))
	// Votes for different targets are no double vote.
	require.False(t, IsDoubleVote(vote(18, 1, 2, "0xaa"), vote(26, 2, 3, "0xbb")))
}

func TestSurroundVote(t *testing.T) {
	// Source 3 to target 6 surrounds source 4 to target 5, in either order.
	require.True(t, IsSurroundVote(vote(48, 3, 6, "0xaa"), vote(40, 4, 5, "0xbb")))
	require.True(t, IsSurroundVote(vote(40, 4, 5, "0xbb"), vote(48, 3, 6, "0xaa")))
	// A shared source or target is no surround.
	require.False(t, IsSurroundVote(vote(48, 4, 6, "0xaa"), vote(40, 4, 5, "0xbb")))
	require.False(t, IsSurroundVote(vote(48, 3, 5, "0xaa"), vote(40, 4, 5, "0xbb")))
	// Consecutive honest votes don't surround each other, nor does a double vote.
	require.False(t, IsSurroundVote(vote(40, 4, 5, "0xaa"), vote(48, 5, 6, "0xbb")))
	require.False(t, IsSurroundVote(vote(18, 1, 2, "0xaa"), vote(18, 1, 2, "0xbb")))
}

func TestDoubleProposal(t *testing.T) {
	header := func(slot, proposer uint64, body string) beacon.BeaconBlockHeaderMessage {
		return beacon.BeaconBlockHeaderMessage{Slot: slot, ProposerIndex: proposer, ParentRoot: "0x10", BodyRoot: body}
	}
	// Two blocks for one slot, as two clients with different graffiti propose.
	require.True(t, IsDoubleProposal(header(19, 5, "0x01"), header(19, 5, "0x02")))
	// The same block signed twice is no offense.
	require.False(t, IsDoubleProposal(header(19, 5, "0x01"), header(19, 5, "0x01")))
	// Blocks of different slots or proposers are not one proposer's two blocks.
	require.False(t, IsDoubleProposal(header(19, 5, "0x01"), header(20, 5, "0x02")))
	require.False(t, IsDoubleProposal(header(19, 5, "0x01"), header(19, 6, "0x02")))
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

	evidence, found := EvidenceAgainst(operations, 64)
	require.True(t, found)
	require.Equal(t, &attester, evidence.Attester)
	require.Nil(t, evidence.Proposer)

	// Validator 7 signed only one of the attestations, so it is not slashed.
	_, found = EvidenceAgainst(operations, 7)
	require.False(t, found)

	evidence, found = EvidenceAgainst(operations, 5)
	require.True(t, found)
	require.Equal(t, &proposer, evidence.Proposer)
	require.Nil(t, evidence.Attester)

	_, found = EvidenceAgainst(beacon.BlockOperations{}, 64)
	require.False(t, found)
}
