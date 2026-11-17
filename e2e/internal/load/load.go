// Package load fills blocks: it keeps the transaction pool stocked with
// transfers until the chain has a run of consecutive full blocks.
package load

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/crypto"
	qrlwallet "github.com/theQRL/go-qrl/crypto/pqcrypto/wallet"
	"github.com/theQRL/go-qrl/params"
	"github.com/theQRL/go-qrl/qrlclient"
	"github.com/theQRL/go-qrl/rpc"
	"golang.org/x/sync/errgroup"
)

const (
	fullPercent         = 95
	poolBlocksAhead     = 3
	warmUpBlocks        = 6
	transfersPerRequest = 128
	sendAttempts        = 3
	feeCapMarginBlocks  = 8
	// alreadyKnown is the pool's error for a transaction it holds.
	alreadyKnown = "already known"
)

var defaultRPCTxFeeCap = big.NewInt(params.Quanta)

var recipient = common.BytesToAddress(crypto.Keccak256([]byte("qrl-tests load recipient")))

var ErrFeeCap = errors.New("the fill needs a fee above the node's transaction fee cap")

// Block is the summary of a block that Fill reads from the chain.
type Block struct {
	Number       uint64
	Hash         common.Hash
	Transactions int
	GasUsed      uint64
	GasLimit     uint64
	EncodedBytes uint64
}

// Full reports whether the block used at least 95% of its gas limit.
func (block Block) Full() bool {
	return block.GasUsed*100 >= block.GasLimit*fullPercent
}

// Shape is the transaction a fill sends.
type Shape struct {
	// Gas is what the transaction uses. A block counts as full by the gas
	// its transactions use, so this has to be what they use, not more.
	Gas uint64
	// To is nil for a transaction that creates a contract.
	To   *common.Address
	Data []byte
}

// Transfers is the shape Fill sends: a plain transfer.
var Transfers = Shape{Gas: params.TxGas, To: &recipient}

// Fill sends transfers from wallet until the chain has the requested number
// of consecutive full blocks, then waits for the transaction pool to empty. It
// returns a summary of each full block; there can be more of them than
// requested.
func Fill(ctx context.Context, client *qrlclient.Client, wallet qrlwallet.Wallet, poll time.Duration, blocks int) ([]Block, error) {
	return FillWith(ctx, client, wallet, poll, blocks, Transfers)
}

// FillWith is Fill with transactions of the given shape.
func FillWith(ctx context.Context, client *qrlclient.Client, wallet qrlwallet.Wallet, poll time.Duration, blocks int, shape Shape) ([]Block, error) {
	if blocks <= 0 {
		return nil, fmt.Errorf("cannot fill %d blocks", blocks)
	}
	if shape.Gas < params.TxGas {
		return nil, fmt.Errorf("a transaction uses at least %d gas, not %d", params.TxGas, shape.Gas)
	}
	filler, err := newFiller(ctx, client, wallet, poll, blocks, shape)
	if err != nil {
		return nil, err
	}
	defer filler.stop()

	if err := filler.fillBlocks(ctx); err != nil {
		return nil, err
	}
	if err := filler.drainPool(ctx); err != nil {
		return nil, err
	}
	return filler.fullBlocks, nil
}

// Steady sends transfers from wallet for the given number of blocks so that
// each block takes about percent of what a full block holds, and returns a
// summary of each of them. A load of at most half of a block leaves the base
// fee where it is, so it can last: full blocks raise the base fee by an eighth
// each, and price themselves out within dozens of blocks.
func Steady(ctx context.Context, client *qrlclient.Client, wallet qrlwallet.Wallet, poll time.Duration, blocks, percent int) ([]Block, error) {
	if blocks <= 0 {
		return nil, fmt.Errorf("cannot load %d blocks", blocks)
	}
	if percent <= 0 || percent > 50 {
		return nil, fmt.Errorf("a steady load is at most half of a block, not %d%%", percent)
	}
	filler, err := newFiller(ctx, client, wallet, poll, 1, Transfers)
	if err != nil {
		return nil, err
	}
	defer filler.stop()

	var (
		perBlock = filler.transfersPerBlock * uint64(percent) / 100
		loaded   []Block
	)
	for len(loaded) < blocks {
		// A block takes what is in the pool when it is built.
		pending, err := filler.pendingTransfers(ctx)
		if err != nil {
			return nil, err
		}
		if pending < perBlock {
			transfers, err := filler.sign(perBlock - pending)
			if err != nil {
				return nil, err
			}
			if err := send(ctx, client, transfers); err != nil {
				return nil, err
			}
			filler.nextNonce += perBlock - pending
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("loading %d blocks, %d so far: %w", blocks, len(loaded), ctx.Err())
		case <-filler.ticker.C:
		}
		latest, err := client.BlockNumber(ctx)
		if err != nil {
			return nil, fmt.Errorf("read block number: %w", err)
		}
		for ; filler.lastBlockRead < latest; filler.lastBlockRead++ {
			block, err := readBlock(ctx, client, filler.lastBlockRead+1)
			if err != nil {
				return nil, err
			}
			loaded = append(loaded, block)
		}
	}
	return loaded, nil
}

type filler struct {
	client          *qrlclient.Client
	wallet          qrlwallet.Wallet
	signer          types.Signer
	tip             *big.Int
	feeCap          *big.Int
	requestedBlocks int
	shape           Shape
	ticker          *time.Ticker

	transfersPerBlock uint64
	transfersLeft     uint64
	nextNonce         uint64
	lastBlockRead     uint64
	fullBlocks        []Block
	runEnded          bool
}

func newFiller(ctx context.Context, client *qrlclient.Client, wallet qrlwallet.Wallet, poll time.Duration, blocks int, shape Shape) (*filler, error) {
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("read chain ID: %w", err)
	}
	head, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("read head: %w", err)
	}
	nextNonce, err := client.PendingNonceAt(ctx, wallet.GetAddress())
	if err != nil {
		return nil, fmt.Errorf("read sender nonce: %w", err)
	}
	tip, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, fmt.Errorf("suggest tip: %w", err)
	}

	maxFullBlocks := blocks + warmUpBlocks + poolBlocksAhead
	feeCap, err := transferFeeCap(head.BaseFee, tip, shape.Gas, maxFullBlocks)
	if err != nil {
		return nil, fmt.Errorf("cannot fill %d blocks: %w", blocks, err)
	}
	transfersPerBlock := head.GasLimit / shape.Gas

	return &filler{
		client:            client,
		wallet:            wallet,
		signer:            types.NewZondSigner(chainID),
		tip:               tip,
		feeCap:            feeCap,
		requestedBlocks:   blocks,
		shape:             shape,
		ticker:            time.NewTicker(poll),
		transfersPerBlock: transfersPerBlock,
		transfersLeft:     uint64(maxFullBlocks) * transfersPerBlock,
		nextNonce:         nextNonce,
		lastBlockRead:     head.Number.Uint64(),
	}, nil
}

func (filler *filler) stop() {
	filler.ticker.Stop()
}

// fillBlocks repeats until enough consecutive blocks are full. The transfers
// cannot all be sent at once: by default a node keeps 5120 pending
// transactions, about five blocks' worth, and drops the rest. So every pass
// refills the pool to poolBlocksAhead blocks' worth, waits, and reads the
// blocks built in the meantime.
func (filler *filler) fillBlocks(ctx context.Context) error {
	for !filler.runComplete() {
		if err := filler.refillPool(ctx); err != nil {
			return err
		}
		if err := filler.waitForNextPoll(ctx); err != nil {
			return err
		}
		if err := filler.trackFullBlocks(ctx); err != nil {
			return err
		}
	}
	return nil
}

// drainPool waits until every transfer sent is in a block, and keeps
// tracking full blocks while it waits.
func (filler *filler) drainPool(ctx context.Context) error {
	for {
		pending, err := filler.pendingTransfers(ctx)
		if err != nil {
			return err
		}
		// Update after counting, so that the block that emptied the pool is in
		// the run.
		if err := filler.trackFullBlocks(ctx); err != nil {
			return err
		}
		if pending == 0 {
			return nil
		}
		if err := filler.waitForNextPoll(ctx); err != nil {
			return err
		}
	}
}

func (filler *filler) waitForNextPoll(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("waiting for %d full blocks and an empty pool, %d full blocks so far: %w", filler.requestedBlocks, len(filler.fullBlocks), ctx.Err())
	case <-filler.ticker.C:
		return nil
	}
}

func (filler *filler) runComplete() bool {
	return len(filler.fullBlocks) >= filler.requestedBlocks
}

// trackFullBlocks reads the blocks built since the last call and updates
// fullBlocks.
func (filler *filler) trackFullBlocks(ctx context.Context) error {
	latest, err := filler.client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("read block number: %w", err)
	}
	for filler.lastBlockRead < latest && !filler.runEnded {
		block, err := readBlock(ctx, filler.client, filler.lastBlockRead+1)
		if err != nil {
			return err
		}
		filler.lastBlockRead++
		switch {
		case block.Full():
			filler.fullBlocks = append(filler.fullBlocks, block)
		case filler.runComplete():
			filler.runEnded = true
		default:
			filler.fullBlocks = nil
		}
	}
	return nil
}

func (filler *filler) pendingTransfers(ctx context.Context) (uint64, error) {
	mined, err := filler.client.NonceAt(ctx, filler.wallet.GetAddress(), nil)
	if err != nil {
		return 0, fmt.Errorf("read sender nonce: %w", err)
	}
	return filler.nextNonce - mined, nil
}

func (filler *filler) refillPool(ctx context.Context) error {
	pending, err := filler.pendingTransfers(ctx)
	if err != nil {
		return err
	}
	want := poolBlocksAhead * filler.transfersPerBlock
	if pending >= want {
		return nil
	}
	count := want - pending
	// The blocks are not filling.
	if count > filler.transfersLeft {
		return fmt.Errorf("transfer budget spent without %d consecutive full blocks, at block %d", filler.requestedBlocks, filler.lastBlockRead)
	}
	transfers, err := filler.sign(count)
	if err != nil {
		return err
	}
	if err := send(ctx, filler.client, transfers); err != nil {
		return err
	}
	filler.nextNonce += count
	filler.transfersLeft -= count
	return nil
}

func (filler *filler) sign(count uint64) ([]*types.Transaction, error) {
	transfers := make([]*types.Transaction, count)
	var group errgroup.Group
	group.SetLimit(runtime.NumCPU())
	for index := range count {
		group.Go(func() error {
			nonce := filler.nextNonce + index
			tx, err := types.SignNewTx(filler.wallet, filler.signer, &types.DynamicFeeTx{
				ChainID:   filler.signer.ChainID(),
				Nonce:     nonce,
				GasTipCap: filler.tip,
				GasFeeCap: filler.feeCap,
				Gas:       filler.shape.Gas,
				To:        filler.shape.To,
				Value:     big.NewInt(1),
				Data:      filler.shape.Data,
			})
			if err != nil {
				return fmt.Errorf("sign transfer %d: %w", nonce, err)
			}
			transfers[index] = tx
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return transfers, nil
}

// transferFeeCap returns a fee cap high enough for transfers of the given gas
// to stay payable after the given number of full blocks has raised the base
// fee.
func transferFeeCap(baseFee, tip *big.Int, gas uint64, fullBlocks int) (*big.Int, error) {
	feeCap := baseFeeAfter(baseFee, fullBlocks+feeCapMarginBlocks)
	feeCap.Add(feeCap, tip)
	// A node refuses a transaction that could pay more than its own limit.
	maxFee := new(big.Int).Mul(feeCap, new(big.Int).SetUint64(gas))
	if maxFee.Cmp(defaultRPCTxFeeCap) > 0 {
		return nil, fmt.Errorf("%w: from a base fee of %s, a transfer's fee would reach %s planck", ErrFeeCap, baseFee, maxFee)
	}
	return feeCap, nil
}

// baseFeeAfter returns the base fee after the given number of full blocks.
// Each one raises it by an eighth, and by at least one.
func baseFeeAfter(baseFee *big.Int, fullBlocks int) *big.Int {
	raised := new(big.Int).Set(baseFee)
	for range fullBlocks {
		rise := new(big.Int).Div(raised, big.NewInt(params.DefaultBaseFeeChangeDenominator))
		if rise.Sign() == 0 {
			rise.SetUint64(1)
		}
		raised.Add(raised, rise)
	}
	return raised
}

func send(ctx context.Context, client *qrlclient.Client, transfers []*types.Transaction) error {
	for batch := range slices.Chunk(transfers, transfersPerRequest) {
		if err := sendBatch(ctx, client, batch); err != nil {
			return err
		}
	}
	return nil
}

// sendBatch sends one request of transfers. A request whose connection drops
// before the reply is sent again: the node may have taken some of it, and
// reports those as already known.
func sendBatch(ctx context.Context, client *qrlclient.Client, batch []*types.Transaction) error {
	for attempt := 1; ; attempt++ {
		elems := make([]rpc.BatchElem, len(batch))
		for index, tx := range batch {
			raw, err := tx.MarshalBinary()
			if err != nil {
				return fmt.Errorf("encode transfer %d: %w", tx.Nonce(), err)
			}
			elems[index] = rpc.BatchElem{
				Method: "qrl_sendRawTransaction",
				Args:   []any{hexutil.Encode(raw)},
				Result: new(common.Hash),
			}
		}
		err := client.Client().BatchCallContext(ctx, elems)
		if err != nil && attempt < sendAttempts && droppedConnection(err) && ctx.Err() == nil {
			continue
		}
		if err != nil {
			return fmt.Errorf("send transfers from %d: %w", batch[0].Nonce(), err)
		}
		for index, elem := range elems {
			if elem.Error != nil && !(attempt > 1 && strings.Contains(elem.Error.Error(), alreadyKnown)) {
				return fmt.Errorf("send transfer %d: %w", batch[index].Nonce(), elem.Error)
			}
		}
		return nil
	}
}

// droppedConnection reports whether a request failed because its connection
// closed before the reply.
func droppedConnection(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET)
}

func readBlock(ctx context.Context, client *qrlclient.Client, number uint64) (Block, error) {
	var raw *struct {
		Number       hexutil.Uint64 `json:"number"`
		Hash         common.Hash    `json:"hash"`
		GasUsed      hexutil.Uint64 `json:"gasUsed"`
		GasLimit     hexutil.Uint64 `json:"gasLimit"`
		Size         hexutil.Uint64 `json:"size"`
		Transactions []common.Hash  `json:"transactions"`
	}
	if err := client.Client().CallContext(ctx, &raw, "qrl_getBlockByNumber", hexutil.EncodeUint64(number), false); err != nil {
		return Block{}, fmt.Errorf("read block %d: %w", number, err)
	}
	if raw == nil {
		return Block{}, fmt.Errorf("block %d not found", number)
	}
	return Block{
		Number:       uint64(raw.Number),
		Hash:         raw.Hash,
		Transactions: len(raw.Transactions),
		GasUsed:      uint64(raw.GasUsed),
		GasLimit:     uint64(raw.GasLimit),
		EncodedBytes: uint64(raw.Size),
	}, nil
}
