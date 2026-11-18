package health

import (
	"context"
	"strconv"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
)

// MissedSlots returns the slots from first to last that have no block.
func MissedSlots(ctx context.Context, client *beacon.Client, first, last uint64) ([]uint64, error) {
	var missed []uint64
	for slot := first; slot <= last; slot++ {
		_, err := client.BlockHeader(ctx, strconv.FormatUint(slot, 10))
		if beacon.IsNotFound(err) {
			missed = append(missed, slot)
			continue
		}
		if err != nil {
			return nil, err
		}
	}
	return missed, nil
}

// HeadVotes is how a slot's committee voted on the slot's own block.
type HeadVotes struct {
	Slot uint64
	// Voted is how many members' attestations are in a block, and ForBlock
	// how many of those name the slot's block as the head.
	Voted, ForBlock int
}

// SlotHeadVotes returns the head votes of each slot from first to last that
// has a block. A block that reached the committee late shows as votes for
// another head. The attestations of a slot are read from the blocks of the
// slots after it, up to the given number of them.
func SlotHeadVotes(ctx context.Context, client *beacon.Client, first, last, inclusionSlots uint64) ([]HeadVotes, error) {
	head, err := client.HeadSlot(ctx)
	if err != nil {
		return nil, err
	}
	attestations := make(map[uint64][]beacon.Attestation)
	roots := make(map[uint64]string)
	for slot := first; slot <= min(last+inclusionSlots, head); slot++ {
		blockID := strconv.FormatUint(slot, 10)
		header, err := client.BlockHeader(ctx, blockID)
		if beacon.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		roots[slot] = header.Root
		block, err := client.BlockOperations(ctx, blockID)
		if err != nil {
			return nil, err
		}
		for _, attestation := range block.Attestations {
			attestations[attestation.Data.Slot] = append(attestations[attestation.Data.Slot], attestation)
		}
	}

	var votes []HeadVotes
	for slot := first; slot <= last; slot++ {
		root, proposed := roots[slot]
		if !proposed {
			continue
		}
		voted, forBlock := make(map[uint64]bool), make(map[uint64]bool)
		for _, attestation := range attestations[slot] {
			for position := range uint64(len(attestation.AggregationBits) * 8) {
				if !attestation.Includes(position) {
					continue
				}
				voted[position] = true
				if attestation.Data.BeaconBlockRoot == root {
					forBlock[position] = true
				}
			}
		}
		votes = append(votes, HeadVotes{Slot: slot, Voted: len(voted), ForBlock: len(forBlock)})
	}
	return votes, nil
}
