package devnet

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/qrlclient"
)

const (
	minAdvancementWindow = 30 * time.Second
	maxAdvancementWindow = 3 * time.Minute
)

func probeNetwork(ctx context.Context, rpcURL, address string) error {
	account, err := common.NewAddressFromString(address)
	if err != nil {
		return fmt.Errorf("parse development wallet address: %w", err)
	}

	client, err := qrlclient.DialContext(ctx, rpcURL)
	if err != nil {
		return fmt.Errorf("dial execution RPC: %w", err)
	}
	defer client.Close()

	firstBlock, err := client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("read block number: %w", err)
	}

	window := advancementWindow(ctx, client, firstBlock)
	advancementCtx, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	if err := retryUntil(advancementCtx, func() error {
		block, err := client.BlockNumber(advancementCtx)
		if err != nil {
			return fmt.Errorf("read advancing block number: %w", err)
		}
		if block <= firstBlock {
			return fmt.Errorf("block number remains at %d", block)
		}
		return nil
	}); err != nil {
		return fmt.Errorf(
			"chain did not advance beyond block %d within %s: %w",
			firstBlock,
			window,
			err,
		)
	}

	balance, err := client.BalanceAt(ctx, account, nil)
	if err != nil {
		return fmt.Errorf("read development wallet balance: %w", err)
	}
	if balance.Sign() <= 0 {
		return fmt.Errorf("development wallet %s has no balance", address)
	}

	return nil
}

// advancementWindow sizes the wait for the next block from the spacing of the
// newest two headers, so slow-slot networks pass and stalled fast networks
// fail promptly; without an observable cadence it falls back to the minimum.
func advancementWindow(ctx context.Context, client *qrlclient.Client, head uint64) time.Duration {
	if head == 0 {
		return minAdvancementWindow
	}

	current, err := client.HeaderByNumber(ctx, new(big.Int).SetUint64(head))
	if err != nil {
		return minAdvancementWindow
	}
	previous, err := client.HeaderByNumber(ctx, new(big.Int).SetUint64(head-1))
	if err != nil {
		return minAdvancementWindow
	}
	if current.Time <= previous.Time {
		return minAdvancementWindow
	}

	window := 2 * time.Duration(current.Time-previous.Time) * time.Second
	return min(max(window, minAdvancementWindow), maxAdvancementWindow)
}
