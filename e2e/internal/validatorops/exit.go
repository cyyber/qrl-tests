package validatorops

import (
	"fmt"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/signing"
	"github.com/theQRL/go-qrl/common/hexutil"
)

// SignVoluntaryExit signs the exit of the validator at index, valid from
// epoch, for a chain with a single fork of the given version.
func (key *Key) SignVoluntaryExit(
	index, epoch uint64, forkVersion signing.ForkVersion, genesisValidatorsRoot signing.Root,
) (beacon.SignedVoluntaryExit, error) {
	domain := signing.ComputeDomain(signing.DomainVoluntaryExit, forkVersion, genesisValidatorsRoot)
	signingRoot := signing.SigningRoot(signing.VoluntaryExit{Epoch: epoch, ValidatorIndex: index}.HashTreeRoot(), domain)
	signature, err := key.Sign(signingRoot[:])
	if err != nil {
		return beacon.SignedVoluntaryExit{}, err
	}
	if err := signing.Verify(signingRoot, key.PublicKey(), signature); err != nil {
		return beacon.SignedVoluntaryExit{}, fmt.Errorf("verify generated exit signature: %w", err)
	}
	return beacon.SignedVoluntaryExit{
		Message:   beacon.VoluntaryExit{Epoch: epoch, ValidatorIndex: index},
		Signature: hexutil.Encode(signature),
	}, nil
}

// SignDoubleProposal signs two headers of one slot, for the validator at index
// as its proposer: the evidence that slashes it.
func (key *Key) SignDoubleProposal(
	index, slot uint64, forkVersion signing.ForkVersion, genesisValidatorsRoot signing.Root,
) (beacon.SignedProposerSlashing, error) {
	domain := signing.ComputeDomain(signing.DomainBeaconProposer, forkVersion, genesisValidatorsRoot)
	var signed [2]beacon.SignedBeaconBlockHeader
	for which := range signed {
		header := signing.BeaconBlockHeader{Slot: slot, ProposerIndex: index, BodyRoot: signing.Root{byte(which + 1)}}
		signingRoot := signing.SigningRoot(header.HashTreeRoot(), domain)
		signature, err := key.Sign(signingRoot[:])
		if err != nil {
			return beacon.SignedProposerSlashing{}, err
		}
		signed[which] = beacon.SignedBeaconBlockHeader{
			Message: beacon.BeaconBlockHeaderMessage{
				Slot:          slot,
				ProposerIndex: index,
				ParentRoot:    hexutil.Encode(header.ParentRoot[:]),
				StateRoot:     hexutil.Encode(header.StateRoot[:]),
				BodyRoot:      hexutil.Encode(header.BodyRoot[:]),
			},
			Signature: hexutil.Encode(signature),
		}
	}
	return beacon.SignedProposerSlashing{SignedHeader1: signed[0], SignedHeader2: signed[1]}, nil
}
