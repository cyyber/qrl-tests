package validatorops

import (
	"fmt"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/signing"
	"github.com/theQRL/go-qrl/common/hexutil"
)

func VoluntaryExit(key *Key, validatorIndex, epoch uint64, chain chaininfo.Info) (beacon.SignedVoluntaryExit, error) {
	exit := signing.VoluntaryExit{Epoch: epoch, ValidatorIndex: validatorIndex}
	exitRoot, err := exit.HashTreeRoot()
	if err != nil {
		return beacon.SignedVoluntaryExit{}, err
	}

	signingRoot := signing.SigningRoot(exitRoot, chain.Domain(signing.DomainVoluntaryExit, epoch))
	signature, err := key.Sign(signingRoot[:])
	if err != nil {
		return beacon.SignedVoluntaryExit{}, err
	}
	if err := signing.Verify(signingRoot, key.PublicKey(), signature); err != nil {
		return beacon.SignedVoluntaryExit{}, fmt.Errorf("verify generated exit signature: %w", err)
	}

	return beacon.SignedVoluntaryExit{
		Message:   beacon.VoluntaryExit{Epoch: epoch, ValidatorIndex: validatorIndex},
		Signature: hexutil.Encode(signature),
	}, nil
}
