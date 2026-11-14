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
	fullPercent         = 95
	poolBlocksAhead     = 3
	warmUpBlocks        = 6
	transfersPerRequest = 128
	feeCapMarginBlocks  = 8
)

var defaultRPCTxFeeCap = big.NewInt(params.Quanta)

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

// Fill sends transfers from wallet until the chain has the requested number
// of consecutive full blocks, then waits for the pool to empty. It returns
// those blocks and any full ones that follow them.
func Fill(ctx context.Context, client *qrlclient.Client, wallet qrlwallet.Wallet, poll time.Duration, blocks int) ([]Block, error) {
	if blocks <= 0 {
		return nil, fmt.Errorf("cannot fill %d blocks", blocks)
	}
	filler, err := newFiller(ctx, client, wallet, blocks)
	if err != nil {
		return nil, err
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		if err := filler.scan(ctx); err != nil {
			return nil, err
		}
		if filler.met() {
			pooled, err := filler.pooled(ctx)
			if err != nil {
				return nil, err
			}
			if pooled == 0 {
				break
			}
		} else if err := filler.topUp(ctx); err != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for %d full blocks and an empty pool, %d full blocks so far: %w", blocks, len(filler.run), ctx.Err())
		case <-ticker.C:
		}
	}
	// The block that emptied the pool can be newer than the last scan.
	if err := filler.scan(ctx); err != nil {
		return nil, err
	}
	return filler.run, nil
}

type filler struct {
	client    *qrlclient.Client
	wallet    qrlwallet.Wallet
	signer    types.Signer
	chainID   *big.Int
	sender    common.Address
	recipient common.Address
	tip       *big.Int
	feeCap    *big.Int
	blocks    int

	transfersPerBlock uint64
	transferBudget    uint64
	transfersSent     uint64
	nextNonce         uint64
	firstBlock        uint64
	scannedBlock      uint64
	run               []Block
	runEnded          bool
}

func newFiller(ctx context.Context, client *qrlclient.Client, wallet qrlwallet.Wallet, blocks int) (*filler, error) {
	sender := common.Address(wallet.GetAddress())
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("read chain ID: %w", err)
	}
	head, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("read head: %w", err)
	}
	tip, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, fmt.Errorf("suggest tip: %w", err)
	}
	budgetBlocks := blocks + warmUpBlocks + poolBlocksAhead
	feeCap, err := feeCapFor(head.BaseFee, tip, budgetBlocks)
	if err != nil {
		return nil, err
	}
	next, err := client.PendingNonceAt(ctx, sender)
	if err != nil {
		return nil, fmt.Errorf("read sender nonce: %w", err)
	}
	perBlock := head.GasLimit / params.TxGas
	return &filler{
		client:            client,
		wallet:            wallet,
		signer:            types.NewZondSigner(chainID),
		chainID:           chainID,
		sender:            sender,
		recipient:         common.BytesToAddress(crypto.Keccak256([]byte("qrl-tests load recipient"))),
		tip:               tip,
		feeCap:            feeCap,
		blocks:            blocks,
		transfersPerBlock: perBlock,
		transferBudget:    uint64(budgetBlocks) * perBlock,
		nextNonce:         next,
		firstBlock:        head.Number.Uint64() + 1,
		scannedBlock:      head.Number.Uint64(),
	}, nil
}

func (filler *filler) met() bool {
	return len(filler.run) >= filler.blocks
}

func (filler *filler) scan(ctx context.Context) error {
	latest, err := filler.client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("read block number: %w", err)
	}
	for ; filler.scannedBlock < latest && !filler.runEnded; filler.scannedBlock++ {
		block, err := readBlock(ctx, filler.client, filler.scannedBlock+1)
		if err != nil {
			return err
		}
		if filler.met() && !block.Full() {
			filler.runEnded = true
			break
		}
		filler.run = extend(filler.run, block)
	}
	return nil
}

func (filler *filler) pooled(ctx context.Context) (uint64, error) {
	mined, err := filler.client.NonceAt(ctx, filler.sender, nil)
	if err != nil {
		return 0, fmt.Errorf("read sender nonce: %w", err)
	}
	return filler.nextNonce - mined, nil
}

func (filler *filler) topUp(ctx context.Context) error {
	pooled, err := filler.pooled(ctx)
	if err != nil {
		return err
	}
	want := poolBlocksAhead * filler.transfersPerBlock
	if pooled >= want {
		return nil
	}
	count := want - pooled
	if filler.transfersSent+count > filler.transferBudget {
		return fmt.Errorf("no run of %d full blocks after %d transfers, from block %d to %d", filler.blocks, filler.transfersSent, filler.firstBlock, filler.scannedBlock)
	}
	transfers, err := filler.sign(count)
	if err != nil {
		return err
	}
	if err := send(ctx, filler.client, transfers); err != nil {
		return err
	}
	filler.nextNonce += count
	filler.transfersSent += count
	return nil
}

func extend(run []Block, block Block) []Block {
	if !block.Full() {
		return nil
	}
	return append(run, block)
}

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

func (filler *filler) sign(count uint64) ([]*types.Transaction, error) {
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
				nonce := filler.nextNonce + index
				tx, err := types.SignNewTx(filler.wallet, filler.signer, &types.DynamicFeeTx{
					ChainID:   filler.chainID,
					Nonce:     nonce,
					GasTipCap: filler.tip,
					GasFeeCap: filler.feeCap,
					Gas:       params.TxGas,
					To:        &filler.recipient,
					Value:     big.NewInt(1),
				})
				if err != nil {
					failures[worker] = fmt.Errorf("sign transfer %d: %w", nonce, err)
					return
				}
				transfers[index] = tx
			}
		}()
	}
	group.Wait()
	return transfers, errors.Join(failures...)
}

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
