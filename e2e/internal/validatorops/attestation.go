package validatorops

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/signing"
	"github.com/theQRL/go-qrl/common/hexutil"
)

// Signer is a validator's key and its index.
type Signer struct {
	Index uint64
	Key   *Key
}

// SignDoubleVote signs two votes of one target epoch for different heads,
// the first by first and the second by second: the evidence that slashes the
// validators among both. The votes are of slot, with the genesis checkpoint
// as source.
func SignDoubleVote(
	first, second []Signer, slot, targetEpoch uint64, forkVersion signing.ForkVersion, genesisValidatorsRoot signing.Root,
) (beacon.SignedAttesterSlashing, error) {
	var slashing beacon.SignedAttesterSlashing
	for which, signers := range [][]Signer{first, second} {
		data := signing.AttestationData{
			Slot:            slot,
			BeaconBlockRoot: signing.Root{byte(which + 1)},
			Target:          signing.Checkpoint{Epoch: targetEpoch},
		}
		signed, err := signIndexedAttestation(signers, data, forkVersion, genesisValidatorsRoot)
		if err != nil {
			return beacon.SignedAttesterSlashing{}, fmt.Errorf("vote %d: %w", which+1, err)
		}
		if which == 0 {
			slashing.Attestation1 = signed
		} else {
			slashing.Attestation2 = signed
		}
	}
	return slashing, nil
}

// signIndexedAttestation has each signer sign data, in index order as an
// indexed attestation lists them.
func signIndexedAttestation(
	signers []Signer, data signing.AttestationData, forkVersion signing.ForkVersion, genesisValidatorsRoot signing.Root,
) (beacon.SignedIndexedAttestation, error) {
	if len(signers) == 0 {
		return beacon.SignedIndexedAttestation{}, fmt.Errorf("an indexed attestation needs a signer")
	}
	sorted := slices.SortedFunc(slices.Values(signers), func(a, b Signer) int { return cmp.Compare(a.Index, b.Index) })
	domain := signing.ComputeDomain(signing.DomainBeaconAttester, forkVersion, genesisValidatorsRoot)
	signingRoot := signing.SigningRoot(data.HashTreeRoot(), domain)
	attestation := beacon.SignedIndexedAttestation{
		Data: beacon.AttestationData{
			Slot:            data.Slot,
			CommitteeIndex:  data.CommitteeIndex,
			BeaconBlockRoot: hexutil.Encode(data.BeaconBlockRoot[:]),
			Source:          beacon.Checkpoint{Epoch: data.Source.Epoch, Root: hexutil.Encode(data.Source.Root[:])},
			Target:          beacon.Checkpoint{Epoch: data.Target.Epoch, Root: hexutil.Encode(data.Target.Root[:])},
		},
	}
	for position, signer := range sorted {
		if position > 0 && signer.Index == sorted[position-1].Index {
			return beacon.SignedIndexedAttestation{}, fmt.Errorf("validator %d signs twice", signer.Index)
		}
		signature, err := signer.Key.Sign(signingRoot[:])
		if err != nil {
			return beacon.SignedIndexedAttestation{}, err
		}
		if err := signing.Verify(signingRoot, signer.Key.PublicKey(), signature); err != nil {
			return beacon.SignedIndexedAttestation{}, fmt.Errorf("verify the vote of validator %d: %w", signer.Index, err)
		}
		attestation.AttestingIndices = append(attestation.AttestingIndices, signer.Index)
		attestation.Signatures = append(attestation.Signatures, hexutil.Encode(signature))
	}
	return attestation, nil
}
