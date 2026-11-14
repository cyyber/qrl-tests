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
	// FullPercent is the share of its gas limit a block uses to count as
	// full. A block is rarely at 100%: the last transfer does not always fit.
	FullPercent = 95

	// The pool is kept this many blocks ahead, so that a block built while
	// more transfers are on their way is still full.
	blocksAhead = 3
	// The blocks that can pass before the first full one: the transfers
	// reach the pool while a block is being built.
	leadInBlocks = 6
	// Transfers sent in one request, which stays well under the 5 MB body
	// limit of the HTTP API: a raw transfer is about 15 KB as hex.
	batchSize = 128
	// The fee cap leaves room for this many full blocks more than the plan.
	feeCapSlackBlocks = 8
)

// rpcFeeCap is the most a transaction may pay in fees for the node to accept
// it over the API: gqrl's default --rpc.txfeecap of one quanta.
var rpcFeeCap = big.NewInt(params.Quanta)

// ErrFeeCap is returned for a plan that would outgrow the fee the node
// accepts: each full block raises the base fee by an eighth.
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
// The pool is topped up as blocks take transfers from it, every poll.
func Fill(ctx context.Context, client *qrlclient.Client, wallet qrlwallet.Wallet, chainID *big.Int, poll time.Duration, plan Plan) (Dense, error) {
	if plan.Blocks <= 0 {
		return Dense{}, fmt.Errorf("plan has %d blocks", plan.Blocks)
	}
	sender := common.Address(wallet.GetAddress())
	head, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return Dense{}, fmt.Errorf("read head: %w", err)
	}
	tip, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return Dense{}, fmt.Errorf("suggest tip: %w", err)
	}
	feeCap, err := feeCapFor(head.BaseFee, tip, plan.Blocks+leadInBlocks+blocksAhead)
	if err != nil {
		return Dense{}, err
	}
	next, err := client.PendingNonceAt(ctx, sender)
	if err != nil {
		return Dense{}, fmt.Errorf("read sender nonce: %w", err)
	}

	var (
		perBlock  = head.GasLimit / params.TxGas
		budget    = uint64(plan.Blocks+leadInBlocks+blocksAhead) * perBlock
		sent      uint64
		recipient = common.BytesToAddress(crypto.Keccak256([]byte("qrl-tests load recipient")))
		signer    = types.NewZondSigner(chainID)
		start     = head.Number.Uint64() + 1
		scanned   = head.Number.Uint64()
		run       []Block
	)
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		// Blocks since the last poll extend the run or start it over.
		latest, err := client.BlockNumber(ctx)
		if err != nil {
			return Dense{}, fmt.Errorf("read block number: %w", err)
		}
		for ; scanned < latest; scanned++ {
			block, err := readBlock(ctx, client, scanned+1)
			if err != nil {
				return Dense{}, err
			}
			run = extend(run, block)
		}
		if len(run) >= plan.Blocks {
			break
		}

		// Top the pool up to blocksAhead blocks of transfers.
		mined, err := client.NonceAt(ctx, sender, nil)
		if err != nil {
			return Dense{}, fmt.Errorf("read sender nonce: %w", err)
		}
		if pooled, want := next-mined, blocksAhead*perBlock; pooled < want {
			count := want - pooled
			if sent+count > budget {
				return Dense{}, fmt.Errorf("no run of %d full blocks after %d transfers, from block %d to %d", plan.Blocks, sent, start, latest)
			}
			transfers, err := sign(wallet, signer, chainID, next, count, tip, feeCap, recipient)
			if err != nil {
				return Dense{}, err
			}
			if err := send(ctx, client, transfers); err != nil {
				return Dense{}, err
			}
			next += count
			sent += count
		}

		select {
		case <-ctx.Done():
			return Dense{}, fmt.Errorf("waiting for %d full blocks, %d so far: %w", plan.Blocks, len(run), ctx.Err())
		case <-ticker.C:
		}
	}

	// The transfers left in the pool fill the next blocks.
	for {
		mined, err := client.NonceAt(ctx, sender, nil)
		if err != nil {
			return Dense{}, fmt.Errorf("read sender nonce: %w", err)
		}
		if mined == next {
			break
		}
		select {
		case <-ctx.Done():
			return Dense{}, fmt.Errorf("waiting for the pool to empty, %d transfers left: %w", next-mined, ctx.Err())
		case <-ticker.C:
		}
	}
	latest, err := client.BlockNumber(ctx)
	if err != nil {
		return Dense{}, fmt.Errorf("read block number: %w", err)
	}
	for ; scanned < latest; scanned++ {
		block, err := readBlock(ctx, client, scanned+1)
		if err != nil {
			return Dense{}, err
		}
		if !block.Full() {
			break
		}
		run = append(run, block)
	}
	return Dense{Blocks: run}, nil
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
	for range blocks + feeCapSlackBlocks {
		// The base fee rises by an eighth, and by at least one.
		rise := new(big.Int).Div(feeCap, big.NewInt(params.DefaultBaseFeeChangeDenominator))
		if rise.Sign() == 0 {
			rise.SetUint64(1)
		}
		feeCap.Add(feeCap, rise)
	}
	feeCap.Add(feeCap, tip)
	if fee := new(big.Int).Mul(feeCap, new(big.Int).SetUint64(params.TxGas)); fee.Cmp(rpcFeeCap) > 0 {
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

// send submits the transfers in nonce order, batchSize to a request.
func send(ctx context.Context, client *qrlclient.Client, transfers []*types.Transaction) error {
	for len(transfers) > 0 {
		batch := transfers[:min(batchSize, len(transfers))]
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
