package syncnode

import (
	"context"
	"fmt"

	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/qrlclient"
)

const invalidPayload = "INVALID"

// SyncToHead offers the execution client the source's head block and names it
// the head, as a beacon node that is behind does, so the client fetches the
// chain up to it from its peers. It returns the head's header without waiting
// for the sync.
func (node *Node) SyncToHead(ctx context.Context, source *qrlclient.Client) (*types.Header, error) {
	head, err := source.BlockByNumber(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("read the source's head block: %w", err)
	}
	if err := node.syncTo(ctx, head); err != nil {
		return nil, err
	}
	return head.Header(), nil
}

func (node *Node) syncTo(ctx context.Context, head *types.Block) error {
	payload, err := executionPayload(head)
	if err != nil {
		return err
	}
	var offered struct {
		Status string `json:"status"`
	}
	if err := node.Engine.CallContext(ctx, &offered, "engine_newPayloadV2", payload); err != nil {
		return fmt.Errorf("offer block %d: %w", head.NumberU64(), err)
	}
	if offered.Status == invalidPayload {
		return fmt.Errorf("the execution client finds block %d invalid", head.NumberU64())
	}

	// Without a beacon node there is no safe or finalized block to name.
	forkchoice := map[string]common.Hash{
		"headBlockHash":      head.Hash(),
		"safeBlockHash":      {},
		"finalizedBlockHash": {},
	}
	var updated struct {
		PayloadStatus struct {
			Status string `json:"status"`
		} `json:"payloadStatus"`
	}
	if err := node.Engine.CallContext(ctx, &updated, "engine_forkchoiceUpdatedV2", forkchoice, nil); err != nil {
		return fmt.Errorf("name block %d the head: %w", head.NumberU64(), err)
	}
	if updated.PayloadStatus.Status == invalidPayload {
		return fmt.Errorf("the execution client finds head block %d invalid", head.NumberU64())
	}
	return nil
}

// executionPayload encodes block as the payload of engine_newPayloadV2.
func executionPayload(block *types.Block) (map[string]any, error) {
	transactions := make([]hexutil.Bytes, len(block.Transactions()))
	for index, tx := range block.Transactions() {
		raw, err := tx.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("encode transaction %s: %w", tx.Hash(), err)
		}
		transactions[index] = raw
	}
	// engine_newPayloadV2 refuses a null list of withdrawals.
	withdrawals := block.Withdrawals()
	if withdrawals == nil {
		withdrawals = types.Withdrawals{}
	}
	header := block.Header()
	return map[string]any{
		"parentHash":    header.ParentHash,
		"feeRecipient":  header.Coinbase,
		"stateRoot":     header.Root,
		"receiptsRoot":  header.ReceiptHash,
		"logsBloom":     hexutil.Bytes(header.Bloom.Bytes()),
		"prevRandao":    header.Random,
		"blockNumber":   hexutil.Uint64(header.Number.Uint64()),
		"gasLimit":      hexutil.Uint64(header.GasLimit),
		"gasUsed":       hexutil.Uint64(header.GasUsed),
		"timestamp":     hexutil.Uint64(header.Time),
		"extraData":     hexutil.Bytes(header.Extra),
		"baseFeePerGas": (*hexutil.Big)(header.BaseFee),
		"blockHash":     block.Hash(),
		"transactions":  transactions,
		"withdrawals":   withdrawals,
	}, nil
}
