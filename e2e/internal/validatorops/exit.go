package validatorops

import (
	"fmt"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/consensuscontext"
	"github.com/cyyber/qrl-tests/e2e/internal/consensuscrypto"
	"github.com/theQRL/go-qrl/common/hexutil"
)

// VoluntaryExit signs an exit for validatorIndex at epoch with key.
func VoluntaryExit(key *Key, validatorIndex, epoch uint64, chain consensuscontext.Context) (beacon.SignedVoluntaryExit, error) {
	exit := consensuscrypto.VoluntaryExit{Epoch: epoch, ValidatorIndex: validatorIndex}
	exitRoot, err := exit.HashTreeRoot()
	if err != nil {
		return beacon.SignedVoluntaryExit{}, err
	}

	signingRoot := consensuscrypto.SigningRoot(exitRoot, chain.Domain(consensuscrypto.DomainVoluntaryExit, epoch))
	signature, err := key.Sign(signingRoot[:])
	if err != nil {
		return beacon.SignedVoluntaryExit{}, err
	}
	if err := consensuscrypto.Verify(signingRoot, key.PublicKey(), signature); err != nil {
		return beacon.SignedVoluntaryExit{}, fmt.Errorf("verify generated exit signature: %w", err)
	}

	return beacon.SignedVoluntaryExit{
		Message:   beacon.VoluntaryExit{Epoch: epoch, ValidatorIndex: validatorIndex},
		Signature: hexutil.Encode(signature),
	}, nil
}
