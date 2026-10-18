// Package history gives a devnet chain content for a syncing node to replay:
// transfers to new accounts, and a contract whose calls fill storage slots
// and emit logs.
package history

import (
	"context"
	"fmt"
	"math/big"

	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/crypto"
	qrlwallet "github.com/theQRL/go-qrl/crypto/pqcrypto/wallet"
	"github.com/theQRL/go-qrl/params"
	"github.com/theQRL/go-qrl/qrlclient"
)

const (
	deployGas = 200_000
	// fillBaseGas and fillSlotGas bound a fill call: an SSTORE to a new slot
	// costs 22,100 gas, and the loop around it a few dozen more.
	fillBaseGas = 50_000
	fillSlotGas = 25_000
)

// Plan sizes the history.
type Plan struct {
	// Transfers each send one planck to a new account.
	Transfers int
	// Fills is how many calls fill storage, SlotsPerFill slots each.
	Fills        int
	SlotsPerFill int
}

// History is what Write put on chain, for a synced node to be compared on.
type History struct {
	Recipients []common.Address
	Contract   common.Address
	// Slots are the storage keys the fills wrote.
	Slots []common.Hash
	// Transactions are every transaction sent, in nonce order. All of them
	// were included and succeeded.
	Transactions []common.Hash
}

// Write sends the plan's transactions from wallet and waits until each one
// is included and has succeeded.
func Write(ctx context.Context, client *qrlclient.Client, wallet qrlwallet.Wallet, chainID *big.Int, plan Plan) (History, error) {
	sender := common.Address(wallet.GetAddress())
	nonce, err := client.PendingNonceAt(ctx, sender)
	if err != nil {
		return History{}, fmt.Errorf("read sender nonce: %w", err)
	}
	head, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return History{}, fmt.Errorf("read head: %w", err)
	}
	tip, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return History{}, fmt.Errorf("suggest tip: %w", err)
	}
	// Room for the base fee to double while the transactions wait.
	feeCap := new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), tip)

	signer := types.LatestSignerForChainID(chainID)
	var history History
	var sent []*types.Transaction
	send := func(to *common.Address, value *big.Int, gas uint64, data []byte) error {
		tx, err := types.SignNewTx(wallet, signer, &types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     nonce,
			GasTipCap: tip,
			GasFeeCap: feeCap,
			Gas:       gas,
			To:        to,
			Value:     value,
			Data:      data,
		})
		if err != nil {
			return fmt.Errorf("sign transaction %d: %w", nonce, err)
		}
		if err := client.SendTransaction(ctx, tx); err != nil {
			return fmt.Errorf("send transaction %d: %w", nonce, err)
		}
		nonce++
		sent = append(sent, tx)
		history.Transactions = append(history.Transactions, tx.Hash())
		return nil
	}

	for index := range plan.Transfers {
		recipient := recipientAddress(index)
		if err := send(&recipient, big.NewInt(1), params.TxGas, nil); err != nil {
			return History{}, err
		}
		history.Recipients = append(history.Recipients, recipient)
	}

	history.Contract = crypto.CreateAddress(sender, nonce)
	if err := send(nil, new(big.Int), deployGas, fillerInitCode()); err != nil {
		return History{}, err
	}
	for fill := range plan.Fills {
		start := uint64(fill * plan.SlotsPerFill)
		gas := uint64(fillBaseGas + plan.SlotsPerFill*fillSlotGas)
		if err := send(&history.Contract, new(big.Int), gas, fillCalldata(start, uint64(plan.SlotsPerFill))); err != nil {
			return History{}, err
		}
		for slot := start; slot < start+uint64(plan.SlotsPerFill); slot++ {
			history.Slots = append(history.Slots, slotKey(slot))
		}
	}

	for _, tx := range sent {
		receipt, err := bind.WaitMined(ctx, client, tx)
		if err != nil {
			return History{}, fmt.Errorf("wait for transaction %d: %w", tx.Nonce(), err)
		}
		if receipt.Status != types.ReceiptStatusSuccessful {
			return History{}, fmt.Errorf("transaction %d failed in block %d", tx.Nonce(), receipt.BlockNumber)
		}
		if tx.To() == nil && receipt.ContractAddress != history.Contract {
			return History{}, fmt.Errorf("contract deployed at %s, not at %s", receipt.ContractAddress, history.Contract)
		}
	}
	return history, nil
}

// recipientAddress is a new account for each index, the same in every run.
func recipientAddress(index int) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(fmt.Sprintf("qrl-tests sync history recipient %d", index))))
}
