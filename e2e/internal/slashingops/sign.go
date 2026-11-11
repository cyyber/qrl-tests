package slashingops

import (
	"fmt"

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
