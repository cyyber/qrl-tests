// Package chaininfo resolves the genesis and fork values of a live network
// that signing domains depend on.
package chaininfo

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/signing"
)

// Source is the subset of the beacon API the info is loaded from.
type Source interface {
	Genesis(context.Context) (beacon.Genesis, error)
	Fork(context.Context) (beacon.Fork, error)
	SpecUint(context.Context, string) (uint64, error)
}

// Info carries the fork and genesis values that domains depend on.
type Info struct {
	SlotsPerEpoch uint64

	genesisValidatorsRoot [signing.RootLength]byte
	genesisForkVersion    [4]byte
	previousVersion       [4]byte
	currentVersion        [4]byte
	forkEpoch             uint64
}

func Load(ctx context.Context, source Source) (Info, error) {
	genesis, err := source.Genesis(ctx)
	if err != nil {
		return Info{}, err
	}

	var chain Info
	if err := decodeFixed("genesis validators root", genesis.ValidatorsRoot, chain.genesisValidatorsRoot[:]); err != nil {
		return Info{}, err
	}
	if err := decodeFixed("genesis fork version", genesis.ForkVersion, chain.genesisForkVersion[:]); err != nil {
		return Info{}, err
	}

	fork, err := source.Fork(ctx)
	if err != nil {
		return Info{}, err
	}
	if err := decodeFixed("previous fork version", fork.PreviousVersion, chain.previousVersion[:]); err != nil {
		return Info{}, err
	}
	if err := decodeFixed("current fork version", fork.CurrentVersion, chain.currentVersion[:]); err != nil {
		return Info{}, err
	}
	chain.forkEpoch = fork.Epoch

	chain.SlotsPerEpoch, err = source.SpecUint(ctx, "SLOTS_PER_EPOCH")
	if err != nil {
		return Info{}, err
	}
	return chain, nil
}

func (chain Info) Epoch(slot uint64) uint64 {
	return slot / chain.SlotsPerEpoch
}

// Domain returns the signing domain for an epoch, honouring the fork version
// active at that epoch.
func (chain Info) Domain(domainType [4]byte, epoch uint64) [signing.RootLength]byte {
	version := chain.currentVersion
	if epoch < chain.forkEpoch {
		version = chain.previousVersion
	}
	return signing.ComputeDomain(domainType, version, chain.genesisValidatorsRoot)
}

// DepositDomain is fork-independent: the genesis fork version with a zero
// genesis validators root, as the deposit contract predates genesis.
func (chain Info) DepositDomain() [signing.RootLength]byte {
	return signing.ComputeDomain(
		signing.DomainDeposit, chain.genesisForkVersion, [signing.RootLength]byte{},
	)
}

func decodeFixed(name, value string, result []byte) error {
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	if len(decoded) != len(result) {
		return fmt.Errorf("invalid %s length %d, want %d", name, len(decoded), len(result))
	}
	copy(result, decoded)
	return nil
}
