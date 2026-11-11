package slashingops

import (
	"slices"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
)

// Offense is the kind of evidence that slashes a validator.
type Offense int

const (
	DoubleVote Offense = iota
	DoubleProposal
	SurroundVote
)

// Evidence is a block's evidence against one validator.
type Evidence struct {
	Attester *beacon.AttesterSlashing
	Proposer *beacon.ProposerSlashing
}

// EvidenceAgainst returns the evidence in a block that slashes the validator
// at index.
func EvidenceAgainst(operations beacon.BlockOperations, index uint64) (Evidence, bool) {
	for position := range operations.AttesterSlashings {
		slashing := &operations.AttesterSlashings[position]
		if slices.Contains(slashing.Attestation1.AttestingIndices, index) &&
			slices.Contains(slashing.Attestation2.AttestingIndices, index) {
			return Evidence{Attester: slashing}, true
		}
	}
	for position := range operations.ProposerSlashings {
		slashing := &operations.ProposerSlashings[position]
		if slashing.Header1.ProposerIndex == index {
			return Evidence{Proposer: slashing}, true
		}
	}
	return Evidence{}, false
}

// IsDoubleVote reports whether the two attestations are different votes for
// the same target epoch, which is slashable.
func IsDoubleVote(first, second beacon.AttestationData) bool {
	return first != second && first.Target.Epoch == second.Target.Epoch
}

// IsSurroundVote reports whether one attestation's source and target
// checkpoints surround the other's, which is slashable.
func IsSurroundVote(first, second beacon.AttestationData) bool {
	return surrounds(first, second) || surrounds(second, first)
}

func surrounds(outer, inner beacon.AttestationData) bool {
	return outer.Source.Epoch < inner.Source.Epoch && inner.Target.Epoch < outer.Target.Epoch
}

// IsDoubleProposal reports whether the two headers are different blocks from
// the same proposer for the same slot, which is slashable.
func IsDoubleProposal(first, second beacon.BeaconBlockHeaderMessage) bool {
	return first != second && first.Slot == second.Slot && first.ProposerIndex == second.ProposerIndex
}
