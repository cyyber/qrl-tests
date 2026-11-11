package validatorops

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/signing"
)

// Signer signs consensus messages with a validator's key, under the domains
// of one chain.
type Signer struct {
	Key                   *Key
	Fork                  signing.Fork
	GenesisValidatorsRoot signing.Root
}

// LoadSigner reads the genesis validators root and the head state's fork,
// which every signature commits to.
func LoadSigner(ctx context.Context, client *beacon.Client, key *Key) (Signer, error) {
	genesis, err := client.Genesis(ctx)
	if err != nil {
		return Signer{}, err
	}
	fork, err := client.Fork(ctx, "head")
	if err != nil {
		return Signer{}, err
	}
	signer := Signer{Key: key, Fork: signing.Fork{Epoch: fork.Epoch}}
	if signer.GenesisValidatorsRoot, err = signing.ParseRoot(genesis.ValidatorsRoot); err != nil {
		return Signer{}, fmt.Errorf("genesis validators root: %w", err)
	}
	if signer.Fork.PreviousVersion, err = signing.ParseForkVersion(fork.PreviousVersion); err != nil {
		return Signer{}, fmt.Errorf("previous fork version: %w", err)
	}
	if signer.Fork.CurrentVersion, err = signing.ParseForkVersion(fork.CurrentVersion); err != nil {
		return Signer{}, fmt.Errorf("current fork version: %w", err)
	}
	return signer, nil
}

// Sign signs objectRoot under the domain of domainType at epoch, checks the
// signature, and returns it 0x-prefixed.
func (signer Signer) Sign(domainType signing.DomainType, epoch uint64, objectRoot signing.Root) (string, error) {
	domain := signer.Fork.Domain(domainType, epoch, signer.GenesisValidatorsRoot)
	signingRoot := signing.SigningRoot(objectRoot, domain)
	signature, err := signer.Key.Sign(signingRoot[:])
	if err != nil {
		return "", err
	}
	if err := signing.Verify(signingRoot, signer.Key.PublicKey(), signature); err != nil {
		return "", fmt.Errorf("verify signature: %w", err)
	}
	return "0x" + hex.EncodeToString(signature), nil
}
