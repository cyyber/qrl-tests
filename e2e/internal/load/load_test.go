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

	feeCap, err := transferFeeCap(baseFee, tip, params.TxGas, fullBlocks)
	require.NoError(t, err)
	want := baseFeeAfter(baseFee, fullBlocks+feeCapMarginBlocks)
	require.Equal(t, want.Add(want, tip), feeCap)

	// A transfer cannot pay more than the node accepts over the API.
	_, err = transferFeeCap(baseFee, tip, params.TxGas, 200)
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

const (
	fakeBlockTransfers = 4
	fakeTransferBytes  = 1000
)

var (
	fakeChainID = big.NewInt(1337)
	fakeBaseFee = big.NewInt(1000)
	fakeTip     = big.NewInt(1)
)

// fakeNode serves the qrl API methods that Fill calls. Its chain grows by one
// block each time Fill asks for the block number, and the new block takes the
// transfers waiting in the pool.
type fakeNode struct {
	blockCapacity  func(number uint64) uint64
	rejectTransfer func(tx *types.Transaction) error

	requests atomic.Int64

	mutex          sync.Mutex
	blockTransfers []uint64
	pooled         uint64
	mined          uint64
}

func startFakeNode(t *testing.T, node *fakeNode) *qrlclient.Client {
	t.Helper()

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
	return (*hexutil.Big)(fakeTip)
}

func (node *fakeNode) BlockNumber() hexutil.Uint64 {
	node.mutex.Lock()
	defer node.mutex.Unlock()

	blockNumber := uint64(len(node.blockTransfers)) + 1
	transfers := min(node.capacity(blockNumber), node.pooled)

	node.pooled -= transfers
	node.mined += transfers
	node.blockTransfers = append(node.blockTransfers, transfers)

	return hexutil.Uint64(blockNumber)
}

func (node *fakeNode) capacity(number uint64) uint64 {
	if node.blockCapacity == nil {
		return fakeBlockTransfers
	}
	return node.blockCapacity(number)
}

func (node *fakeNode) GetBlockByNumber(number rpc.BlockNumber, _ bool) map[string]any {
	node.mutex.Lock()
	defer node.mutex.Unlock()

	if number == rpc.LatestBlockNumber {
		number = rpc.BlockNumber(len(node.blockTransfers))
	}
	if number < 0 || int(number) > len(node.blockTransfers) {
		return nil
	}

	var transfers uint64
	if number > 0 {
		transfers = node.blockTransfers[number-1]
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
		"baseFeePerGas":    (*hexutil.Big)(fakeBaseFee),
		"withdrawalsRoot":  types.EmptyWithdrawalsHash,
		"size":             hexutil.Uint64(fakeTransferBytes * transfers),
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
	if node.rejectTransfer != nil {
		if err := node.rejectTransfer(tx); err != nil {
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

func signingFiller(t *testing.T, nonce uint64) *filler {
	t.Helper()
	return &filler{
		wallet:    newWallet(t),
		signer:    types.NewZondSigner(fakeChainID),
		tip:       fakeTip,
		feeCap:    big.NewInt(100),
		nextNonce: nonce,
		shape:     Transfers,
	}
}

func fill(t *testing.T, node *fakeNode, poll, timeout time.Duration) ([]Block, error) {
	t.Helper()
	client := startFakeNode(t, node)
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	return Fill(ctx, client, newWallet(t), poll, fillBlocks)
}

const fillBlocks = 3

func TestFill(t *testing.T) {
	for _, test := range []struct {
		name         string
		partialBlock uint64
	}{
		{name: "every block full"},
		{name: "a block that is not full", partialBlock: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			node := &fakeNode{blockCapacity: func(number uint64) uint64 {
				if number == test.partialBlock {
					return 1
				}
				return fakeBlockTransfers
			}}
			run, err := fill(t, node, time.Millisecond, 30*time.Second)
			require.NoError(t, err)

			// Fill returns with the pool empty. The run can be longer than asked:
			// the transfers still in the pool fill more blocks after it.
			require.Zero(t, node.pooled)
			require.GreaterOrEqual(t, len(run), fillBlocks)
			require.Greater(t, run[0].Number, test.partialBlock, "the run starts after the block that is not full")
			for index, block := range run {
				require.True(t, block.Full(), "block %d", block.Number)
				require.Equal(t, fakeBlockTransfers, block.Transactions)
				require.Equal(t, uint64(fakeTransferBytes*fakeBlockTransfers), block.EncodedBytes)
				if index > 0 {
					require.Equal(t, run[index-1].Number+1, block.Number)
				}
			}

			lastBlock := run[len(run)-1].Number
			for index, transfers := range node.blockTransfers[lastBlock:] {
				require.Zero(t, transfers, "block %d, after the run, has transfers", lastBlock+uint64(index)+1)
			}
		})
	}
}

func TestFillFails(t *testing.T) {
	for _, test := range []struct {
		name    string
		node    *fakeNode
		poll    time.Duration
		timeout time.Duration
		err     string
	}{
		{
			name: "blocks that do not fill",
			node: &fakeNode{blockCapacity: func(uint64) uint64 { return fakeBlockTransfers / 2 }},
			poll: time.Millisecond, timeout: 30 * time.Second,
			err: fmt.Sprintf("transfer budget spent without %d consecutive full blocks", fillBlocks),
		},
		{
			name: "a rejected transfer",
			node: &fakeNode{rejectTransfer: func(tx *types.Transaction) error {
				if tx.Nonce() == 5 {
					return errors.New("txpool is full")
				}
				return nil
			}},
			poll: time.Millisecond, timeout: 30 * time.Second,
			err: "send transfer 5: txpool is full",
		},
		{
			// The context ends during the wait for the next poll.
			name: "a context that ends",
			node: new(fakeNode),
			poll: time.Hour, timeout: time.Second,
			err: fmt.Sprintf("waiting for %d full blocks and an empty pool, 0 full blocks so far: %s",
				fillBlocks, context.DeadlineExceeded),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := fill(t, test.node, test.poll, test.timeout)
			require.ErrorContains(t, err, test.err)
			sent := test.node.mined + test.node.pooled
			maxFullBlocks := fillBlocks + warmUpBlocks + poolBlocksAhead
			require.LessOrEqual(t, sent, uint64(maxFullBlocks*fakeBlockTransfers), "Fill sent more than its budget")
		})
	}
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

func TestSignWithAShape(t *testing.T) {
	filler := signingFiller(t, 0)
	filler.shape = Shape{Gas: 90_000, Data: []byte{0x60, 0x00}}
	transfers, err := filler.sign(2)
	require.NoError(t, err)
	for _, tx := range transfers {
		require.Equal(t, uint64(90_000), tx.Gas())
		require.Nil(t, tx.To(), "a shape without a recipient creates a contract")
		require.Equal(t, []byte{0x60, 0x00}, tx.Data())
	}
}

func TestFillWithRefusesTooLittleGas(t *testing.T) {
	_, err := FillWith(t.Context(), nil, nil, time.Millisecond, 1, Shape{Gas: params.TxGas - 1})
	require.ErrorContains(t, err, "at least 21000 gas")
}

func TestSteady(t *testing.T) {
	const blocks, percent = 6, 50
	node := new(fakeNode)
	client := startFakeNode(t, node)
	loaded, err := Steady(t.Context(), client, newWallet(t), time.Millisecond, blocks, percent)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(loaded), blocks)
	for index, block := range loaded {
		require.LessOrEqual(t, block.Transactions, fakeBlockTransfers*percent/100, "block %d", block.Number)
		if index > 0 {
			require.Equal(t, loaded[index-1].Number+1, block.Number)
		}
	}
	// The pool never holds more than the load of one block.
	require.LessOrEqual(t, node.pooled, uint64(fakeBlockTransfers*percent/100))
	require.Positive(t, node.mined)
}

func TestSteadyRefusesMoreThanHalfABlock(t *testing.T) {
	_, err := Steady(t.Context(), nil, nil, time.Millisecond, 1, 51)
	require.ErrorContains(t, err, "at most half of a block")
	_, err = Steady(t.Context(), nil, nil, time.Millisecond, 0, 50)
	require.ErrorContains(t, err, "cannot load 0 blocks")
}
