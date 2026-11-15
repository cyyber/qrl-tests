package load

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
	qrlwallet "github.com/theQRL/go-qrl/crypto/pqcrypto/wallet"
	"github.com/theQRL/go-qrl/params"
	"github.com/theQRL/go-qrl/qrlclient"
	"github.com/theQRL/go-qrl/rpc"
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

func TestTransferFeeCap(t *testing.T) {
	baseFee, tip := big.NewInt(1_000_000_000), big.NewInt(1)
	const fullBlocks = 10

	feeCap, err := transferFeeCap(baseFee, tip, fullBlocks)
	require.NoError(t, err)
	want := baseFeeAfter(baseFee, fullBlocks+feeCapMarginBlocks)
	require.Equal(t, want.Add(want, tip), feeCap)

	// A transfer cannot pay more than the node accepts over the API.
	_, err = transferFeeCap(baseFee, tip, 200)
	require.ErrorIs(t, err, ErrFeeCap)
}

func TestBaseFeeAfter(t *testing.T) {
	baseFee := big.NewInt(800)
	require.Equal(t, big.NewInt(1012), baseFeeAfter(baseFee, 2), "800, then 900, then 1012")
	require.Equal(t, big.NewInt(800), baseFeeAfter(baseFee, 0))
	require.Equal(t, big.NewInt(800), baseFee, "the base fee passed in is not changed")
	// A base fee too low for an eighth of it to count still rises by one.
	require.Equal(t, big.NewInt(10), baseFeeAfter(big.NewInt(7), 3))
}

// The fake node's blocks hold a few transfers, so that a test signs few.
const fakeBlockTransfers = 4

var fakeChainID = big.NewInt(1337)

// fakeNode is the qrl API of a node as Fill uses it. It builds a block each
// time its block number is read: the block takes the transfers the pool has,
// up to its capacity.
type fakeNode struct {
	baseFee *big.Int
	// capacity is how many transfers the block with the given number takes;
	// nil is as many as a block holds.
	capacity func(number uint64) uint64
	// reject, when set, is the pool's answer to a transfer.
	reject func(tx *types.Transaction) error

	requests atomic.Int64

	mutex  sync.Mutex
	blocks []uint64 // transfers in each block, from block 1
	pooled uint64
	mined  uint64
}

func startFakeNode(t *testing.T, node *fakeNode) *qrlclient.Client {
	t.Helper()
	if node.baseFee == nil {
		node.baseFee = big.NewInt(1000)
	}
	api := rpc.NewServer()
	t.Cleanup(api.Stop)
	require.NoError(t, api.RegisterName("qrl", node))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		node.requests.Add(1)
		api.ServeHTTP(writer, request)
	}))
	t.Cleanup(server.Close)
	client, err := qrlclient.Dial(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

func (node *fakeNode) ChainId() *hexutil.Big {
	return (*hexutil.Big)(fakeChainID)
}

func (node *fakeNode) MaxPriorityFeePerGas() *hexutil.Big {
	return (*hexutil.Big)(big.NewInt(1))
}

// BlockNumber adds a block with the pool's transfers, up to the block's
// capacity, and returns its number.
func (node *fakeNode) BlockNumber() hexutil.Uint64 {
	node.mutex.Lock()
	defer node.mutex.Unlock()
	taken := uint64(fakeBlockTransfers)
	if node.capacity != nil {
		taken = node.capacity(uint64(len(node.blocks)) + 1)
	}
	taken = min(taken, node.pooled)
	node.pooled -= taken
	node.mined += taken
	node.blocks = append(node.blocks, taken)
	return hexutil.Uint64(len(node.blocks))
}

func (node *fakeNode) GetBlockByNumber(number rpc.BlockNumber, _ bool) map[string]any {
	node.mutex.Lock()
	defer node.mutex.Unlock()
	if number == rpc.LatestBlockNumber {
		number = rpc.BlockNumber(len(node.blocks))
	}
	if number < 0 || int(number) > len(node.blocks) {
		return nil
	}
	var transfers uint64
	if number > 0 {
		transfers = node.blocks[number-1]
	}
	return map[string]any{
		"number":           hexutil.Uint64(number),
		"hash":             common.BigToHash(big.NewInt(int64(number) + 1)),
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

func (node *fakeNode) GetTransactionCount(_ common.Address, block rpc.BlockNumber) hexutil.Uint64 {
	node.mutex.Lock()
	defer node.mutex.Unlock()
	if block == rpc.PendingBlockNumber {
		return hexutil.Uint64(node.mined + node.pooled)
	}
	return hexutil.Uint64(node.mined)
}

func (node *fakeNode) SendRawTransaction(raw hexutil.Bytes) (common.Hash, error) {
	node.mutex.Lock()
	defer node.mutex.Unlock()
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		return common.Hash{}, err
	}
	// The pool only takes the next nonce: Fill sends in nonce order.
	if want := node.mined + node.pooled; tx.Nonce() != want {
		return common.Hash{}, fmt.Errorf("transfer has nonce %d, want %d", tx.Nonce(), want)
	}
	if node.reject != nil {
		if err := node.reject(tx); err != nil {
			return common.Hash{}, err
		}
	}
	node.pooled++
	return tx.Hash(), nil
}

func newWallet(t *testing.T) qrlwallet.Wallet {
	t.Helper()
	wallet, err := qrlwallet.Generate(qrlwallet.ML_DSA_87)
	require.NoError(t, err)
	return wallet
}

// signingFiller is a filler with what sign needs.
func signingFiller(t *testing.T, nonce uint64) *filler {
	t.Helper()
	return &filler{
		wallet:    newWallet(t),
		signer:    types.NewZondSigner(fakeChainID),
		tip:       big.NewInt(1),
		feeCap:    big.NewInt(100),
		nextNonce: nonce,
	}
}

func fill(t *testing.T, node *fakeNode, blocks int) ([]Block, error) {
	t.Helper()
	client := startFakeNode(t, node)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return Fill(ctx, client, newWallet(t), time.Millisecond, blocks)
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
		require.Equal(t, uint64(1000*fakeBlockTransfers), block.EncodedBytes)
		if index > 0 {
			require.Equal(t, run[index-1].Number+1, block.Number)
		}
	}
	require.Equal(t, node.mined, uint64(len(run))*fakeBlockTransfers, "every transfer sent is in a block of the run")
}

func TestFillStartsOverAfterABlockThatIsNotFull(t *testing.T) {
	const blocks, partial = 3, 2
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
	require.ErrorContains(t, err, "transfer budget spent without 3 consecutive full blocks")
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// The context ends during the wait for the next poll.
	_, err := Fill(ctx, client, newWallet(t), time.Hour, 3)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "waiting for 3 full blocks and an empty pool, 0 full blocks so far")
}

func TestSign(t *testing.T) {
	const nonce, count = 7, 40
	filler := signingFiller(t, nonce)
	transfers, err := filler.sign(count)
	require.NoError(t, err)
	require.Len(t, transfers, count)
	for index, tx := range transfers {
		require.Equal(t, uint64(nonce+index), tx.Nonce())
		sender, err := types.Sender(filler.signer, tx)
		require.NoError(t, err)
		require.Equal(t, common.Address(filler.wallet.GetAddress()), sender)
	}
}

func TestSendSplitsTheTransfersAcrossRequests(t *testing.T) {
	node := new(fakeNode)
	client := startFakeNode(t, node)
	const count = 2*transfersPerRequest + 1
	transfers, err := signingFiller(t, 0).sign(count)
	require.NoError(t, err)

	require.NoError(t, send(t.Context(), client, transfers))
	require.EqualValues(t, 3, node.requests.Load())
	// The fake pool takes a transfer only in nonce order.
	require.EqualValues(t, count, node.pooled)
}
