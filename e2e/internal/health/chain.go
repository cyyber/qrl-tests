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
