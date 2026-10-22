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
	GenesisTime   time.Time
	SlotsPerEpoch uint64
	SlotDuration  time.Duration
	EpochDuration time.Duration
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
	chain.GenesisTime = time.Unix(int64(genesis.Time), 0)

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
	chain.EpochDuration = time.Duration(chain.SlotsPerEpoch) * chain.SlotDuration
	chain.ExecutionVotingPeriod = time.Duration(votingEpochs) * chain.EpochDuration
	chain.ExecutionFollowDistance = time.Duration(followBlocks*secondsPerBlock) * time.Second
	return chain, nil
}

func (chain Info) Epoch(slot uint64) uint64 {
	return slot / chain.SlotsPerEpoch
}

// SlotPoint is a moment on the chain's clock: a slot and how far into it.
type SlotPoint struct {
	Slot   uint64
	Offset time.Duration
}

func (point SlotPoint) String() string {
	return fmt.Sprintf("slot %d at %.3fs", point.Slot, point.Offset.Seconds())
}

// SlotPointAt places a wall-clock time on the chain's clock. Before genesis
// it is slot 0 with a negative offset.
func (chain Info) SlotPointAt(at time.Time) SlotPoint {
	elapsed := at.Sub(chain.GenesisTime)
	if elapsed < 0 {
		return SlotPoint{Offset: elapsed}
	}
	return SlotPoint{Slot: uint64(elapsed / chain.SlotDuration), Offset: elapsed % chain.SlotDuration}
}

// SlotFilter says which slots a wait may end in.
type SlotFilter func(slot uint64) bool

// AnySlot accepts every slot.
func AnySlot(uint64) bool { return true }

// LastSlotOfEpoch accepts the last slot of each epoch. At its end the epoch
// transition runs, with the justification and finalization updates and the
// state save a restart loads from.
func (chain Info) LastSlotOfEpoch(slot uint64) bool {
	return slot%chain.SlotsPerEpoch == chain.SlotsPerEpoch-1
}

// NextSlotOffset returns the first time at or after from that is offset into
// a slot the filter accepts. offset must be at least zero and shorter than a
// slot, and the filter must accept a slot in every epoch.
func (chain Info) NextSlotOffset(from time.Time, offset time.Duration, accept SlotFilter) (time.Time, error) {
	if offset < 0 || offset >= chain.SlotDuration {
		return time.Time{}, fmt.Errorf("slot offset %s is outside a %s slot", offset, chain.SlotDuration)
	}
	point := chain.SlotPointAt(from)
	next := from.Add(offset - point.Offset)
	if point.Offset > offset {
		next = next.Add(chain.SlotDuration)
	}
	for range chain.SlotsPerEpoch {
		if accept(chain.SlotPointAt(next).Slot) {
			return next, nil
		}
		next = next.Add(chain.SlotDuration)
	}
	return time.Time{}, fmt.Errorf("no slot accepted in the epoch after %s", chain.SlotPointAt(from))
}

// WaitForSlotOffset waits until the wall clock next reaches offset into a
// slot the filter accepts, and returns where on the chain's clock it woke
// up: the slot it waited for, a little past the offset.
func (chain Info) WaitForSlotOffset(ctx context.Context, offset time.Duration, accept SlotFilter) (SlotPoint, error) {
	deadline, err := chain.NextSlotOffset(time.Now(), offset, accept)
	if err != nil {
		return SlotPoint{}, err
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-timer.C:
		return chain.SlotPointAt(time.Now()), nil
	case <-ctx.Done():
		return SlotPoint{}, ctx.Err()
	}
}

// DepositWait is how long a deposit takes to reach the beacon state on a
// healthy chain. A voting period's votes cover deposits made at least the
// follow distance before it began, and take effect once they hold a majority,
// about halfway through. Four more epochs cover a few missed proposals,
// inclusion and the effective balance update. It assumes the period starts at
// least two follow distances after genesis; before that, votes keep the
// genesis execution data.
func (chain Info) DepositWait() time.Duration {
	return chain.ExecutionFollowDistance + chain.ExecutionVotingPeriod*3/2 + 4*chain.EpochDuration
}

// GenesisForkVersion is the fork version deposits are signed for, as
// 0x-prefixed hex.
func (chain Info) GenesisForkVersion() string {
	return "0x" + hex.EncodeToString(chain.genesisForkVersion[:])
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
