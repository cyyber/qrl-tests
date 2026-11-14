package load

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
	qrlwallet "github.com/theQRL/go-qrl/crypto/pqcrypto/wallet"
	"github.com/theQRL/go-qrl/params"
	"github.com/theQRL/go-qrl/qrlclient"
)

// The fake node's blocks hold a few transfers, so that a test signs few.
const fakeBlockTransfers = 4

// fakeNode is the JSON-RPC API of a node as Fill uses it. It builds a block
// each time its block number is read: the block takes the transfers the pool
// has, up to its capacity.
type fakeNode struct {
	t       *testing.T
	baseFee *big.Int
	// capacity is how many transfers the block with the given number takes;
	// nil is as many as a block holds.
	capacity func(number uint64) uint64

	mu     sync.Mutex
	blocks []uint64 // transfers in each block, from block 1
	pooled uint64
	mined  uint64
}

func startFakeNode(t *testing.T, node *fakeNode) *qrlclient.Client {
	t.Helper()
	node.t = t
	if node.baseFee == nil {
		node.baseFee = big.NewInt(1000)
	}
	server := httptest.NewServer(node)
	t.Cleanup(server.Close)
	client, err := qrlclient.Dial(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

type rpcRequest struct {
	ID     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

func (node *fakeNode) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	var raw json.RawMessage
	if err := json.NewDecoder(request.Body).Decode(&raw); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	answer := func(call rpcRequest) map[string]any {
		result, err := node.call(call.Method, call.Params)
		if err != nil {
			return map[string]any{"jsonrpc": "2.0", "id": call.ID, "error": map[string]any{"code": -32000, "message": err.Error()}}
		}
		return map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result}
	}
	var batch []rpcRequest
	if json.Unmarshal(raw, &batch) == nil {
		answers := make([]map[string]any, len(batch))
		for index, call := range batch {
			answers[index] = answer(call)
		}
		_ = json.NewEncoder(writer).Encode(answers)
		return
	}
	var call rpcRequest
	if err := json.Unmarshal(raw, &call); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(writer).Encode(answer(call))
}

func (node *fakeNode) call(method string, args []json.RawMessage) (any, error) {
	node.mu.Lock()
	defer node.mu.Unlock()
	switch method {
	case "qrl_blockNumber":
		node.build()
		return hexutil.Uint64(len(node.blocks)), nil
	case "qrl_getBlockByNumber":
		var tag string
		if err := json.Unmarshal(args[0], &tag); err != nil {
			return nil, err
		}
		number := uint64(len(node.blocks))
		if tag != "latest" {
			parsed, err := hexutil.DecodeUint64(tag)
			if err != nil {
				return nil, err
			}
			number = parsed
		}
		return node.block(number), nil
	case "qrl_maxPriorityFeePerGas":
		return "0x1", nil
	case "qrl_getTransactionCount":
		var tag string
		if err := json.Unmarshal(args[1], &tag); err != nil {
			return nil, err
		}
		if tag == "pending" {
			return hexutil.Uint64(node.mined + node.pooled), nil
		}
		return hexutil.Uint64(node.mined), nil
	case "qrl_sendRawTransaction":
		var raw hexutil.Bytes
		if err := json.Unmarshal(args[0], &raw); err != nil {
			return nil, err
		}
		var tx types.Transaction
		if err := tx.UnmarshalBinary(raw); err != nil {
			return nil, err
		}
		// The pool only takes the next nonce: Fill sends in nonce order.
		if want := node.mined + node.pooled; tx.Nonce() != want {
			return nil, fmt.Errorf("transfer has nonce %d, want %d", tx.Nonce(), want)
		}
		node.pooled++
		return tx.Hash(), nil
	}
	return nil, fmt.Errorf("the fake node has no method %s", method)
}

// build adds a block with the pool's transfers, up to the block's capacity.
func (node *fakeNode) build() {
	taken := uint64(fakeBlockTransfers)
	if node.capacity != nil {
		taken = node.capacity(uint64(len(node.blocks)) + 1)
	}
	taken = min(taken, node.pooled)
	node.pooled -= taken
	node.mined += taken
	node.blocks = append(node.blocks, taken)
}

func (node *fakeNode) block(number uint64) map[string]any {
	if number > uint64(len(node.blocks)) {
		return nil
	}
	var transfers uint64
	if number > 0 {
		transfers = node.blocks[number-1]
	}
	return map[string]any{
		"number":           hexutil.Uint64(number),
		"hash":             common.BigToHash(new(big.Int).SetUint64(number + 1)),
		"parentHash":       common.Hash{},
		"miner":            common.Address{},
		"stateRoot":        common.Hash{},
		"transactionsRoot": common.Hash{},
		"receiptsRoot":     common.Hash{},
		"logsBloom":        hexutil.Bytes(make([]byte, types.BloomByteLength)),
		"gasLimit":         hexutil.Uint64(fakeBlockTransfers * params.TxGas),
		"gasUsed":          hexutil.Uint64(transfers * params.TxGas),
		"timestamp":        hexutil.Uint64(number),
		"extraData":        hexutil.Bytes{},
		"prevRandao":       common.Hash{},
		"baseFeePerGas":    (*hexutil.Big)(node.baseFee),
		"withdrawalsRoot":  types.EmptyWithdrawalsHash,
		"size":             hexutil.Uint64(1000 * transfers),
		"transactions":     make([]common.Hash, transfers),
		"withdrawals":      []any{},
	}
}

func fill(t *testing.T, node *fakeNode, blocks int) (Dense, error) {
	t.Helper()
	client := startFakeNode(t, node)
	wallet, err := qrlwallet.Generate(qrlwallet.ML_DSA_87)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return Fill(ctx, client, wallet, big.NewInt(1337), time.Millisecond, Plan{Blocks: blocks})
}

func TestFill(t *testing.T) {
	const blocks = 3
	node := &fakeNode{}
	dense, err := fill(t, node, blocks)
	require.NoError(t, err)

	// The pool is empty, and the run is every full block: the plan's, then
	// the ones the transfers left in the pool filled.
	require.Zero(t, node.pooled)
	require.GreaterOrEqual(t, len(dense.Blocks), blocks)
	for index, block := range dense.Blocks {
		require.True(t, block.Full(), "block %d", block.Number)
		require.Equal(t, fakeBlockTransfers, block.Transactions)
		require.Equal(t, uint64(1000*fakeBlockTransfers), block.Size)
		if index > 0 {
			require.Equal(t, dense.Blocks[index-1].Number+1, block.Number)
		}
	}
	require.Equal(t, node.mined, uint64(len(dense.Blocks))*fakeBlockTransfers, "every transfer sent is in a block of the run")
}

func TestFillStartsOverAfterABlockThatIsNotFull(t *testing.T) {
	const blocks, partial = 3, 4
	node := &fakeNode{capacity: func(number uint64) uint64 {
		if number == partial {
			return 1
		}
		return fakeBlockTransfers
	}}
	dense, err := fill(t, node, blocks)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(dense.Blocks), blocks)
	require.Greater(t, dense.Blocks[0].Number, uint64(partial), "the run starts after the block that is not full")
}

func TestFillGivesUpWhenBlocksDoNotFill(t *testing.T) {
	node := &fakeNode{capacity: func(uint64) uint64 { return fakeBlockTransfers / 2 }}
	_, err := fill(t, node, 3)
	require.ErrorContains(t, err, "no run of 3 full blocks")
	// The plan's budget bounds what was sent.
	require.LessOrEqual(t, node.mined+node.pooled, uint64((3+warmUpBlocks+poolBlocksAhead)*fakeBlockTransfers))
}

func TestFillRefusesAPlanAboveTheFeeCap(t *testing.T) {
	node := &fakeNode{baseFee: new(big.Int).Div(defaultRPCTxFeeCap, new(big.Int).SetUint64(params.TxGas))}
	_, err := fill(t, node, 3)
	require.ErrorIs(t, err, ErrFeeCap)
	require.Zero(t, node.pooled, "nothing is sent")
}

func TestFillRejectsAnEmptyPlan(t *testing.T) {
	_, err := Fill(context.Background(), nil, nil, nil, time.Millisecond, Plan{})
	require.ErrorContains(t, err, "plan has 0 blocks")
}

func TestSign(t *testing.T) {
	wallet, err := qrlwallet.Generate(qrlwallet.ML_DSA_87)
	require.NoError(t, err)
	var (
		chainID = big.NewInt(1337)
		signer  = types.NewZondSigner(chainID)
		to      = common.Address{1}
	)
	const nonce, count = 7, 40
	transfers, err := sign(wallet, signer, chainID, nonce, count, big.NewInt(1), big.NewInt(100), to)
	require.NoError(t, err)
	require.Len(t, transfers, count)
	for index, tx := range transfers {
		require.Equal(t, uint64(nonce+index), tx.Nonce())
		sender, err := types.Sender(signer, tx)
		require.NoError(t, err)
		require.Equal(t, common.Address(wallet.GetAddress()), sender)
	}
}
