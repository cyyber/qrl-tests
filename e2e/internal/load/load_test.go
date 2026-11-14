package load

import (
	"context"
	"encoding/json"
	"errors"
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

func TestBlockFull(t *testing.T) {
	for _, test := range []struct {
		name    string
		gasUsed uint64
		full    bool
	}{
		{"at the gas limit", 20_000_000, true},
		{"one transfer short of the gas limit", 20_000_000 - params.TxGas, true},
		{"at the full share", 19_000_000, true},
		{"just under the full share", 18_999_999, false},
		{"empty", 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.full, Block{GasUsed: test.gasUsed, GasLimit: 20_000_000}.Full())
		})
	}
}

func TestExtend(t *testing.T) {
	full := Block{GasUsed: 20_000_000, GasLimit: 20_000_000}
	partial := Block{GasUsed: 10_000_000, GasLimit: 20_000_000}

	var run []Block
	for number, block := range []Block{full, full, partial, full} {
		block.Number = uint64(number)
		run = extend(run, block)
	}
	// The block that is not full starts the run over.
	require.Len(t, run, 1)
	require.Equal(t, uint64(3), run[0].Number)
}

func TestFeeCapFor(t *testing.T) {
	tip := big.NewInt(1)

	// The fee cap is above the base fee that many full blocks lead to.
	baseFee := big.NewInt(1_000_000_000)
	const blocks = 10
	feeCap, err := feeCapFor(baseFee, tip, blocks)
	require.NoError(t, err)
	reached := new(big.Int).Set(baseFee)
	for range blocks {
		reached.Add(reached, new(big.Int).Div(reached, big.NewInt(params.DefaultBaseFeeChangeDenominator)))
	}
	require.Equal(t, 1, feeCap.Cmp(reached), "fee cap %s, base fee after %d full blocks %s", feeCap, blocks, reached)

	// A base fee too low for an eighth of it to count still rises by one.
	feeCap, err = feeCapFor(big.NewInt(7), tip, blocks)
	require.NoError(t, err)
	require.Equal(t, 1, feeCap.Cmp(big.NewInt(7+blocks)))

	// A transfer cannot pay more than the node accepts over the API.
	_, err = feeCapFor(baseFee, tip, 200)
	require.ErrorIs(t, err, ErrFeeCap)
}

// The fake node's blocks hold a few transfers, so that a test signs few.
const fakeBlockTransfers = 4

var fakeChainID = big.NewInt(1337)

// fakeNode is the JSON-RPC API of a node as Fill uses it. It builds a block
// each time its block number is read: the block takes the transfers the pool
// has, up to its capacity.
type fakeNode struct {
	baseFee *big.Int
	// capacity is how many transfers the block with the given number takes;
	// nil is as many as a block holds.
	capacity func(number uint64) uint64
	// reject, when set, is the pool's answer to a transfer.
	reject func(tx *types.Transaction) error

	mu     sync.Mutex
	blocks []uint64 // transfers in each block, from block 1
	pooled uint64
	mined  uint64
}

func startFakeNode(t *testing.T, node *fakeNode) *qrlclient.Client {
	t.Helper()
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
	case "qrl_chainId":
		return (*hexutil.Big)(fakeChainID), nil
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
		if node.reject != nil {
			if err := node.reject(&tx); err != nil {
				return nil, err
			}
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

func fill(t *testing.T, node *fakeNode, blocks int) ([]Block, error) {
	t.Helper()
	client := startFakeNode(t, node)
	wallet, err := qrlwallet.Generate(qrlwallet.ML_DSA_87)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return Fill(ctx, client, wallet, time.Millisecond, blocks)
}

func TestFill(t *testing.T) {
	const blocks = 3
	node := &fakeNode{}
	run, err := fill(t, node, blocks)
	require.NoError(t, err)

	// The pool is empty, and the run is every full block: the ones asked for,
	// then the ones the transfers left in the pool filled.
	require.Zero(t, node.pooled)
	require.GreaterOrEqual(t, len(run), blocks)
	for index, block := range run {
		require.True(t, block.Full(), "block %d", block.Number)
		require.Equal(t, fakeBlockTransfers, block.Transactions)
		require.Equal(t, uint64(1000*fakeBlockTransfers), block.Size)
		if index > 0 {
			require.Equal(t, run[index-1].Number+1, block.Number)
		}
	}
	require.Equal(t, node.mined, uint64(len(run))*fakeBlockTransfers, "every transfer sent is in a block of the run")
}

func TestFillStartsOverAfterABlockThatIsNotFull(t *testing.T) {
	const blocks, partial = 3, 4
	node := &fakeNode{capacity: func(number uint64) uint64 {
		if number == partial {
			return 1
		}
		return fakeBlockTransfers
	}}
	run, err := fill(t, node, blocks)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(run), blocks)
	require.Greater(t, run[0].Number, uint64(partial), "the run starts after the block that is not full")
}

func TestFillGivesUpWhenBlocksDoNotFill(t *testing.T) {
	node := &fakeNode{capacity: func(uint64) uint64 { return fakeBlockTransfers / 2 }}
	_, err := fill(t, node, 3)
	require.ErrorContains(t, err, "no run of 3 full blocks")
	// The budget bounds what was sent.
	require.LessOrEqual(t, node.mined+node.pooled, uint64((3+warmUpBlocks+poolBlocksAhead)*fakeBlockTransfers))
}

func TestFillRefusesAFeeAboveTheNodesCap(t *testing.T) {
	node := &fakeNode{baseFee: new(big.Int).Div(defaultRPCTxFeeCap, new(big.Int).SetUint64(params.TxGas))}
	_, err := fill(t, node, 3)
	require.ErrorIs(t, err, ErrFeeCap)
	require.Zero(t, node.pooled, "nothing is sent")
}

func TestFillRejectsNoBlocks(t *testing.T) {
	_, err := Fill(context.Background(), nil, nil, time.Millisecond, 0)
	require.ErrorContains(t, err, "cannot fill 0 blocks")
}

func TestFillFailsOnARejectedTransfer(t *testing.T) {
	node := &fakeNode{reject: func(tx *types.Transaction) error {
		if tx.Nonce() == 5 {
			return errors.New("txpool is full")
		}
		return nil
	}}
	_, err := fill(t, node, 3)
	require.ErrorContains(t, err, "send transfer 5: txpool is full")
}

func TestFillStopsWithItsContext(t *testing.T) {
	client := startFakeNode(t, new(fakeNode))
	wallet, err := qrlwallet.Generate(qrlwallet.ML_DSA_87)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// The context ends during the wait for the next poll.
	_, err = Fill(ctx, client, wallet, time.Hour, 3)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "waiting for 3 full blocks and an empty pool, 0 full blocks so far")
}

func TestSign(t *testing.T) {
	wallet, err := qrlwallet.Generate(qrlwallet.ML_DSA_87)
	require.NoError(t, err)
	const nonce, count = 7, 40
	filler := &filler{
		wallet:    wallet,
		signer:    types.NewZondSigner(fakeChainID),
		chainID:   fakeChainID,
		recipient: common.Address{1},
		tip:       big.NewInt(1),
		feeCap:    big.NewInt(100),
		nextNonce: nonce,
	}
	transfers, err := filler.sign(count)
	require.NoError(t, err)
	require.Len(t, transfers, count)
	for index, tx := range transfers {
		require.Equal(t, uint64(nonce+index), tx.Nonce())
		sender, err := types.Sender(filler.signer, tx)
		require.NoError(t, err)
		require.Equal(t, common.Address(wallet.GetAddress()), sender)
	}
}
