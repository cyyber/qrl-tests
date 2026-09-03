package soak

import (
	"context"
	"math/big"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/internal/devwallet"
	"github.com/cyyber/qrl-tests/internal/soak"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/qrlclient"
)

// canary sends a 1-shor self-transfer through the development wallet.
type canary struct {
	client *qrlclient.Client
	from   common.Address
	signer bind.SignerFn
}

func newCanary(ctx context.Context, environment devnet.Environment) (*canary, error) {
	participant, err := environment.Primary()
	if err != nil {
		return nil, err
	}
	client, err := qrlclient.DialContext(ctx, participant.Execution.RPCURL)
	if err != nil {
		return nil, err
	}
	wallet, err := devwallet.Restore()
	if err != nil {
		client.Close()
		return nil, err
	}
	chainID, err := client.ChainID(ctx)
	if err != nil {
		client.Close()
		return nil, err
	}
	transactor, err := bind.NewKeyedTransactorWithChainID(wallet, chainID)
	if err != nil {
		client.Close()
		return nil, err
	}
	return &canary{client: client, from: transactor.From, signer: transactor.Signer}, nil
}

func (probe *canary) Close() {
	if probe != nil && probe.client != nil {
		probe.client.Close()
	}
}

func (probe *canary) Send(ctx context.Context, timeout time.Duration) soak.Canary {
	result := soak.Canary{SentAt: time.Now().UTC()}
	wait, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tx, err := sendValue(wait, probe.client, probe.from, probe.signer)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	receipt, err := bind.WaitMined(wait, probe.client, tx)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if receipt == nil || receipt.Status != types.ReceiptStatusSuccessful {
		result.Error = "transaction was mined unsuccessful"
		return result
	}
	result.Included = true
	result.Latency = time.Since(result.SentAt)
	if receipt.BlockNumber != nil {
		result.Block = receipt.BlockNumber.Uint64()
	}
	return result
}

func sendValue(ctx context.Context, client *qrlclient.Client, from common.Address, signer bind.SignerFn) (*types.Transaction, error) {
	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return nil, err
	}
	tip, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, err
	}
	header, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, err
	}
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, err
	}
	base := big.NewInt(0)
	if header.BaseFee != nil {
		base = header.BaseFee
	}
	feeCap := new(big.Int).Add(new(big.Int).Mul(base, big.NewInt(2)), tip)
	unsigned := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: tip,
		GasFeeCap: feeCap,
		Gas:       21_000,
		To:        &from,
		Value:     big.NewInt(1),
	})
	signed, err := signer(from, unsigned)
	if err != nil {
		return nil, err
	}
	if err := client.SendTransaction(ctx, signed); err != nil {
		return nil, err
	}
	return signed, nil
}
