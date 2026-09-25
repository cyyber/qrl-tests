// Package chaininfo reads the deposit signing domain and the chain's timing
// from a live beacon node.
package chaininfo

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/signing"
)

// Source is the subset of the beacon API the info is loaded from.
type Source interface {
	Genesis(context.Context) (beacon.Genesis, error)
	SpecUint(context.Context, string) (uint64, error)
}

// Info describes a live chain: its genesis fork version and its timing.
type Info struct {
	SlotsPerEpoch uint64
	SlotDuration  time.Duration
	// ExecutionVotingPeriod is how long the beacon chain votes on one
	// execution-data snapshot; deposits only count once a vote settles.
	ExecutionVotingPeriod time.Duration
	// ExecutionFollowDistance is how long before a voting period starts a
	// deposit must land for that period's votes to cover it.
	ExecutionFollowDistance time.Duration

	genesisForkVersion signing.ForkVersion
}

func Load(ctx context.Context, source Source) (Info, error) {
	genesis, err := source.Genesis(ctx)
	if err != nil {
		return Info{}, err
	}

	var chain Info
	if err := decodeFixed("genesis fork version", genesis.ForkVersion, chain.genesisForkVersion[:]); err != nil {
		return Info{}, err
	}

	chain.SlotsPerEpoch, err = source.SpecUint(ctx, "SLOTS_PER_EPOCH")
	if err != nil {
		return Info{}, err
	}
	secondsPerSlot, err := source.SpecUint(ctx, "SECONDS_PER_SLOT")
	if err != nil {
		return Info{}, err
	}
	votingEpochs, err := source.SpecUint(ctx, "EPOCHS_PER_EXECUTION_VOTING_PERIOD")
	if err != nil {
		return Info{}, err
	}
	followBlocks, err := source.SpecUint(ctx, "EXECUTION_FOLLOW_DISTANCE")
	if err != nil {
		return Info{}, err
	}
	secondsPerBlock, err := source.SpecUint(ctx, "SECONDS_PER_EXECUTION_BLOCK")
	if err != nil {
		return Info{}, err
	}
	chain.SlotDuration = time.Duration(secondsPerSlot) * time.Second
	chain.ExecutionVotingPeriod = time.Duration(votingEpochs*chain.SlotsPerEpoch) * chain.SlotDuration
	chain.ExecutionFollowDistance = time.Duration(followBlocks*secondsPerBlock) * time.Second
	return chain, nil
}

func (chain Info) Epoch(slot uint64) uint64 {
	return slot / chain.SlotsPerEpoch
}

// DepositWait is how long a deposit takes to reach the beacon state on a
// healthy chain. A voting period's votes cover deposits made at least the
// follow distance before it began, and take effect once they hold a majority,
// about halfway through. Four more epochs cover a few missed proposals,
// inclusion and the effective balance update. It assumes the period starts at
// least two follow distances after genesis; before that, votes keep the
// genesis execution data.
func (chain Info) DepositWait() time.Duration {
	epoch := time.Duration(chain.SlotsPerEpoch) * chain.SlotDuration
	return chain.ExecutionFollowDistance + chain.ExecutionVotingPeriod*3/2 + 4*epoch
}

// DepositDomain is fork-independent: the genesis fork version with a zero
// genesis validators root, as the deposit contract predates genesis.
func (chain Info) DepositDomain() signing.Domain {
	return signing.ComputeDomain(
		signing.DomainDeposit, chain.genesisForkVersion, signing.Root{},
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
