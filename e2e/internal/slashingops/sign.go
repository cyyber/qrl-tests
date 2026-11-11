package slashingops

import (
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/signing"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
)

// SignProposerSlashing signs two headers as their proposer and returns them as
// proposer slashing evidence. slotsPerEpoch gives the epoch whose domain a
// header is signed under.
func SignProposerSlashing(
	signer validatorops.Signer, slotsPerEpoch uint64, first, second beacon.BeaconBlockHeaderMessage,
) (beacon.SignedProposerSlashing, error) {
	signedFirst, err := signHeader(signer, slotsPerEpoch, first)
	if err != nil {
		return beacon.SignedProposerSlashing{}, err
	}
	signedSecond, err := signHeader(signer, slotsPerEpoch, second)
	if err != nil {
		return beacon.SignedProposerSlashing{}, err
	}
	return beacon.SignedProposerSlashing{SignedHeader1: signedFirst, SignedHeader2: signedSecond}, nil
}

func signHeader(
	signer validatorops.Signer, slotsPerEpoch uint64, header beacon.BeaconBlockHeaderMessage,
) (beacon.SignedBeaconBlockHeader, error) {
	root, err := headerRoot(header)
	if err != nil {
		return beacon.SignedBeaconBlockHeader{}, err
	}
	signature, err := signer.Sign(signing.DomainBeaconProposer, header.Slot/slotsPerEpoch, root)
	if err != nil {
		return beacon.SignedBeaconBlockHeader{}, err
	}
	return beacon.SignedBeaconBlockHeader{Message: header, Signature: signature}, nil
}

// SignAttestation signs data as the validator at the duty's committee position,
// and returns the attestation that member alone casts.
func SignAttestation(
	signer validatorops.Signer, duty beacon.AttesterDuty, data beacon.AttestationData,
) (beacon.SignedAttestation, error) {
	root, err := attestationDataRoot(data)
	if err != nil {
		return beacon.SignedAttestation{}, err
	}
	signature, err := signer.Sign(signing.DomainBeaconAttester, data.Target.Epoch, root)
	if err != nil {
		return beacon.SignedAttestation{}, err
	}
	return beacon.SignedAttestation{
		AggregationBits: "0x" + hex.EncodeToString(beacon.SingleMemberBits(duty.CommitteeLength, duty.ValidatorCommitteeIndex)),
		Data:            data,
		Signatures:      []string{signature},
	}, nil
}

// SignAttesterSlashing signs two votes as the validator at index and returns
// them as attester slashing evidence against it alone.
func SignAttesterSlashing(
	signer validatorops.Signer, index uint64, first, second beacon.AttestationData,
) (beacon.SignedAttesterSlashing, error) {
	signedFirst, err := signIndexed(signer, index, first)
	if err != nil {
		return beacon.SignedAttesterSlashing{}, err
	}
	signedSecond, err := signIndexed(signer, index, second)
	if err != nil {
		return beacon.SignedAttesterSlashing{}, err
	}
	return beacon.SignedAttesterSlashing{Attestation1: signedFirst, Attestation2: signedSecond}, nil
}

func signIndexed(signer validatorops.Signer, index uint64, data beacon.AttestationData) (beacon.SignedIndexedAttestation, error) {
	root, err := attestationDataRoot(data)
	if err != nil {
		return beacon.SignedIndexedAttestation{}, err
	}
	signature, err := signer.Sign(signing.DomainBeaconAttester, data.Target.Epoch, root)
	if err != nil {
		return beacon.SignedIndexedAttestation{}, err
	}
	return beacon.SignedIndexedAttestation{
		AttestingIndices: []string{strconv.FormatUint(index, 10)},
		Data:             data,
		Signatures:       []string{signature},
	}, nil
}

func attestationDataRoot(data beacon.AttestationData) (signing.Root, error) {
	var roots [3]signing.Root
	for position, value := range []string{data.BeaconBlockRoot, data.Source.Root, data.Target.Root} {
		root, err := signing.ParseRoot(value)
		if err != nil {
			return signing.Root{}, fmt.Errorf("attestation of slot %d: %w", data.Slot, err)
		}
		roots[position] = root
	}
	return signing.AttestationData{
		Slot:            data.Slot,
		CommitteeIndex:  data.CommitteeIndex,
		BeaconBlockRoot: roots[0],
		Source:          signing.Checkpoint{Epoch: data.Source.Epoch, Root: roots[1]},
		Target:          signing.Checkpoint{Epoch: data.Target.Epoch, Root: roots[2]},
	}.HashTreeRoot(), nil
}

func headerRoot(header beacon.BeaconBlockHeaderMessage) (signing.Root, error) {
	var roots [3]signing.Root
	for position, value := range []string{header.ParentRoot, header.StateRoot, header.BodyRoot} {
		root, err := signing.ParseRoot(value)
		if err != nil {
			return signing.Root{}, fmt.Errorf("header of slot %d: %w", header.Slot, err)
		}
		roots[position] = root
	}
	return signing.BeaconBlockHeader{
		Slot:          header.Slot,
		ProposerIndex: header.ProposerIndex,
		ParentRoot:    roots[0],
		StateRoot:     roots[1],
		BodyRoot:      roots[2],
	}.HashTreeRoot(), nil
}
