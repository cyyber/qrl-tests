// Package load fills blocks: it keeps the pool stocked with transfers until
// the chain has a run of consecutive full blocks.
package load

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"sync"
	"time"

	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/crypto"
	qrlwallet "github.com/theQRL/go-qrl/crypto/pqcrypto/wallet"
	"github.com/theQRL/go-qrl/params"
	"github.com/theQRL/go-qrl/qrlclient"
	"github.com/theQRL/go-qrl/rpc"
)

const (
	// FullPercent is the share of its gas limit a full block uses.
	FullPercent = 95

	poolBlocksAhead     = 3
	warmUpBlocks        = 6
	transfersPerRequest = 128
	feeCapMarginBlocks  = 8
)

var defaultRPCTxFeeCap = big.NewInt(params.Quanta)

var ErrFeeCap = errors.New("the plan needs a fee above the node's transaction fee cap")

// Plan sizes the load.
type Plan struct {
	// Blocks is how many consecutive full blocks to produce.
	Blocks int
}

// Block is a block of the dense run.
type Block struct {
	Number       uint64
	Hash         common.Hash
	Transactions int
	GasUsed      uint64
	GasLimit     uint64
	// Size is the length of the block's encoding.
	Size uint64
}

// Full reports whether the block used at least FullPercent of its gas limit.
func (block Block) Full() bool {
	return block.GasUsed*100 >= block.GasLimit*FullPercent
}

// Dense is a run of consecutive full blocks, oldest first.
type Dense struct {
	Blocks []Block
}

// Fill sends transfers from wallet until the chain has plan.Blocks consecutive
// full blocks, then waits until every transfer it sent is in a block. It
// returns the run, which can be longer than the plan: the transfers still in
// the pool when the plan is met fill the blocks after it.
//
// The chain is read and the pool topped up every poll.
//
// Every transfer is from the one wallet, in nonce order, so the pool holds at
// most poolBlocksAhead blocks of them: under the 5120 pending transactions
// gqrl keeps by default, above which it drops those of the accounts with the
// most.
func Fill(ctx context.Context, client *qrlclient.Client, wallet qrlwallet.Wallet, chainID *big.Int, poll time.Duration, plan Plan) (Dense, error) {
	if plan.Blocks <= 0 {
		return Dense{}, fmt.Errorf("plan has %d blocks", plan.Blocks)
	}
	filler, err := newFiller(ctx, client, wallet, chainID, plan)
	if err != nil {
		return Dense{}, err
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		if err := filler.scan(ctx); err != nil {
			return Dense{}, err
		}
		if !filler.met() {
			if err := filler.topUp(ctx); err != nil {
				return Dense{}, err
			}
		} else if left, err := filler.pooled(ctx); err != nil {
			return Dense{}, err
		} else if left == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return Dense{}, fmt.Errorf("waiting for %d full blocks and an empty pool, %d full blocks so far: %w", plan.Blocks, len(filler.run), ctx.Err())
		case <-ticker.C:
		}
	}
	// The block that emptied the pool can be newer than the last scan.
	if err := filler.scan(ctx); err != nil {
		return Dense{}, err
	}
	return Dense{Blocks: filler.run}, nil
}

// filler is the state of one Fill.
type filler struct {
	client    *qrlclient.Client
	wallet    qrlwallet.Wallet
	signer    types.Signer
	chainID   *big.Int
	sender    common.Address
	recipient common.Address
	tip       *big.Int
	feeCap    *big.Int
	plan      Plan

	// perBlock is how many transfers a block holds.
	perBlock uint64
	// budget is how many transfers the plan may send, and sent how many it has.
	budget, sent uint64
	// next is the nonce of the next transfer.
	next uint64
	// first is the first block the fill could reach, and scanned the last
	// block read.
	first, scanned uint64
	// run is the current run of consecutive full blocks, and closed whether a
	// block that is not full has ended it after the plan was met.
	run    []Block
	closed bool
}

func newFiller(ctx context.Context, client *qrlclient.Client, wallet qrlwallet.Wallet, chainID *big.Int, plan Plan) (*filler, error) {
	sender := common.Address(wallet.GetAddress())
	head, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("read head: %w", err)
	}
	tip, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, fmt.Errorf("suggest tip: %w", err)
	}
	blocks := plan.Blocks + warmUpBlocks + poolBlocksAhead
	feeCap, err := feeCapFor(head.BaseFee, tip, blocks)
	if err != nil {
		return nil, err
	}
	next, err := client.PendingNonceAt(ctx, sender)
	if err != nil {
		return nil, fmt.Errorf("read sender nonce: %w", err)
	}
	perBlock := head.GasLimit / params.TxGas
	return &filler{
		client:    client,
		wallet:    wallet,
		signer:    types.NewZondSigner(chainID),
		chainID:   chainID,
		sender:    sender,
		recipient: common.BytesToAddress(crypto.Keccak256([]byte("qrl-tests load recipient"))),
		tip:       tip,
		feeCap:    feeCap,
		plan:      plan,
		perBlock:  perBlock,
		budget:    uint64(blocks) * perBlock,
		next:      next,
		first:     head.Number.Uint64() + 1,
		scanned:   head.Number.Uint64(),
	}, nil
}

// met reports whether the run is as long as the plan.
func (filler *filler) met() bool {
	return len(filler.run) >= filler.plan.Blocks
}

// scan reads the blocks since the last scan. Until the plan is met, a block
// that is not full starts the run over; after that, it ends the run.
func (filler *filler) scan(ctx context.Context) error {
	latest, err := filler.client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("read block number: %w", err)
	}
	for ; filler.scanned < latest && !filler.closed; filler.scanned++ {
		block, err := readBlock(ctx, filler.client, filler.scanned+1)
		if err != nil {
			return err
		}
		if filler.met() && !block.Full() {
			filler.closed = true
			break
		}
		filler.run = extend(filler.run, block)
	}
	return nil
}

// pooled returns how many of the transfers sent are not in a block yet.
func (filler *filler) pooled(ctx context.Context) (uint64, error) {
	mined, err := filler.client.NonceAt(ctx, filler.sender, nil)
	if err != nil {
		return 0, fmt.Errorf("read sender nonce: %w", err)
	}
	return filler.next - mined, nil
}

// topUp sends the transfers the pool is short of poolBlocksAhead blocks. It
// fails once the plan's budget is spent: the blocks are not filling.
func (filler *filler) topUp(ctx context.Context) error {
	pooled, err := filler.pooled(ctx)
	if err != nil {
		return err
	}
	want := poolBlocksAhead * filler.perBlock
	if pooled >= want {
		return nil
	}
	count := want - pooled
	if filler.sent+count > filler.budget {
		return fmt.Errorf("no run of %d full blocks after %d transfers, from block %d to %d", filler.plan.Blocks, filler.sent, filler.first, filler.scanned)
	}
	transfers, err := sign(filler.wallet, filler.signer, filler.chainID, filler.next, count, filler.tip, filler.feeCap, filler.recipient)
	if err != nil {
		return err
	}
	if err := send(ctx, filler.client, transfers); err != nil {
		return err
	}
	filler.next += count
	filler.sent += count
	return nil
}

// extend adds a block to a run of consecutive full blocks. A block that is
// not full starts the run over.
func extend(run []Block, block Block) []Block {
	if !block.Full() {
		return nil
	}
	return append(run, block)
}

// feeCapFor returns a fee cap that stays above the base fee while blocks more
// full blocks each raise it by an eighth, with some to spare. It fails with
// ErrFeeCap if a transfer would then pay more than the node accepts.
func feeCapFor(baseFee, tip *big.Int, blocks int) (*big.Int, error) {
	feeCap := new(big.Int).Set(baseFee)
	for range blocks + feeCapMarginBlocks {
		// The base fee rises by an eighth, and by at least one.
		rise := new(big.Int).Div(feeCap, big.NewInt(params.DefaultBaseFeeChangeDenominator))
		if rise.Sign() == 0 {
			rise.SetUint64(1)
		}
		feeCap.Add(feeCap, rise)
	}
	feeCap.Add(feeCap, tip)
	if fee := new(big.Int).Mul(feeCap, new(big.Int).SetUint64(params.TxGas)); fee.Cmp(defaultRPCTxFeeCap) > 0 {
		return nil, fmt.Errorf("%w: %d full blocks from a base fee of %s take a transfer's fee to %s planck", ErrFeeCap, blocks, baseFee, fee)
	}
	return feeCap, nil
}

// sign signs count transfers of one planck from nonce on. Signing is most of
// the work of a transfer, so every processor takes a share.
func sign(wallet qrlwallet.Wallet, signer types.Signer, chainID *big.Int, nonce, count uint64, tip, feeCap *big.Int, to common.Address) ([]*types.Transaction, error) {
	var (
		transfers = make([]*types.Transaction, count)
		failures  = make([]error, runtime.NumCPU())
		group     sync.WaitGroup
	)
	for worker := range failures {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := uint64(worker); index < count; index += uint64(len(failures)) {
				tx, err := types.SignNewTx(wallet, signer, &types.DynamicFeeTx{
					ChainID:   chainID,
					Nonce:     nonce + index,
					GasTipCap: tip,
					GasFeeCap: feeCap,
					Gas:       params.TxGas,
					To:        &to,
					Value:     big.NewInt(1),
				})
				if err != nil {
					failures[worker] = fmt.Errorf("sign transfer %d: %w", nonce+index, err)
					return
				}
				transfers[index] = tx
			}
		}()
	}
	group.Wait()
	return transfers, errors.Join(failures...)
}

// send submits the transfers in nonce order, transfersPerRequest to a request.
func send(ctx context.Context, client *qrlclient.Client, transfers []*types.Transaction) error {
	for len(transfers) > 0 {
		batch := transfers[:min(transfersPerRequest, len(transfers))]
		transfers = transfers[len(batch):]
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
		if err := client.Client().BatchCallContext(ctx, elems); err != nil {
			return fmt.Errorf("send transfers from %d: %w", batch[0].Nonce(), err)
		}
		for index, elem := range elems {
			if elem.Error != nil {
				return fmt.Errorf("send transfer %d: %w", batch[index].Nonce(), elem.Error)
			}
		}
	}
	return nil
}

// readBlock reads a block's gas, size and transaction count.
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
		Size:         uint64(raw.Size),
	}, nil
}
