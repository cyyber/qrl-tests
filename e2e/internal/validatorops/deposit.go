package validatorops

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/signing"
	"github.com/theQRL/go-qrl"
	"github.com/theQRL/go-qrl/accounts/abi"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/params"
)

const depositContractABI = `[
  {"anonymous":false,"inputs":[{"indexed":false,"name":"pubkey","type":"bytes"},{"indexed":false,"name":"withdrawal_recipient","type":"bytes"},{"indexed":false,"name":"amount","type":"bytes"},{"indexed":false,"name":"randao_commitment","type":"bytes"},{"indexed":false,"name":"signature","type":"bytes"},{"indexed":false,"name":"index","type":"bytes"}],"name":"DepositEvent","type":"event"},
  {"inputs":[{"name":"pubkey","type":"bytes"},{"name":"withdrawal_recipient","type":"bytes"},{"name":"randao_commitment","type":"bytes"},{"name":"signature","type":"bytes"},{"name":"deposit_data_root","type":"bytes32"}],"name":"deposit","outputs":[],"stateMutability":"payable","type":"function"}
]`

type depositEvent struct {
	PublicKey           []byte `abi:"pubkey"`
	WithdrawalRecipient []byte `abi:"withdrawal_recipient"`
	Amount              []byte `abi:"amount"`
	RandaoCommitment    []byte `abi:"randao_commitment"`
	Signature           []byte `abi:"signature"`
	Index               []byte `abi:"index"`
}

// Depositor submits deposits from the development wallet.
type Depositor struct {
	node     *live.Node
	contract *bind.BoundContract
	address  common.Address
	domain   signing.Domain
}

func NewDepositor(ctx context.Context, node *live.Node, chain chaininfo.Info) (*Depositor, error) {
	contract, err := node.Beacon.DepositContract(ctx)
	if err != nil {
		return nil, err
	}
	address, err := common.NewAddressFromString(contract.Address)
	if err != nil {
		return nil, fmt.Errorf("parse deposit contract address: %w", err)
	}

	parsed, err := abi.JSON(strings.NewReader(depositContractABI))
	if err != nil {
		return nil, fmt.Errorf("parse deposit contract ABI: %w", err)
	}

	return &Depositor{
		node:     node,
		contract: bind.NewBoundContract(address, parsed, node.Execution, node.Execution, node.Execution),
		address:  address,
		domain:   chain.DepositDomain(),
	}, nil
}

// Deposit stakes amountInShor for key with the given withdrawal recipient and
// waits for the transaction to be mined with a matching DepositEvent.
func (depositor *Depositor) Deposit(ctx context.Context, key *Key, withdrawalRecipient common.Address, amountInShor uint64) (*types.Receipt, error) {
	data, root, err := depositInput(key, withdrawalRecipient, amountInShor, depositor.domain)
	if err != nil {
		return nil, err
	}

	auth, err := bind.NewKeyedTransactorWithChainID(depositor.node.Wallet, depositor.node.ChainID)
	if err != nil {
		return nil, err
	}
	auth.Context = ctx
	auth.Value = new(big.Int).Mul(new(big.Int).SetUint64(amountInShor), big.NewInt(params.Shor))

	transaction, err := depositor.contract.Transact(
		auth, "deposit", data.PublicKey, data.WithdrawalRecipient, data.RandaoCommitment, data.Signature, root,
	)
	if err != nil {
		return nil, fmt.Errorf("submit deposit: %w", err)
	}

	receipt, err := bind.WaitMined(ctx, depositor.node.Execution, transaction)
	if err != nil {
		return nil, err
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return nil, fmt.Errorf("deposit transaction %s failed", transaction.Hash())
	}

	if err := depositor.verifyEvent(receipt, data); err != nil {
		return nil, err
	}
	return receipt, nil
}

func (depositor *Depositor) verifyEvent(receipt *types.Receipt, data signing.DepositData) error {
	for _, log := range receipt.Logs {
		if log.Address != depositor.address {
			continue
		}
		var event depositEvent
		if err := depositor.contract.UnpackLog(&event, "DepositEvent", *log); err != nil {
			return fmt.Errorf("decode deposit event: %w", err)
		}
		amount, err := event.amount()
		if err != nil {
			return err
		}
		if amount != data.Amount {
			return fmt.Errorf("deposit event amount is %d shor, want %d", amount, data.Amount)
		}
		if !bytes.Equal(event.PublicKey, data.PublicKey) ||
			!bytes.Equal(event.WithdrawalRecipient, data.WithdrawalRecipient) ||
			!bytes.Equal(event.RandaoCommitment, data.RandaoCommitment) ||
			!bytes.Equal(event.Signature, data.Signature) {
			return errors.New("deposit event does not match the signed deposit data")
		}
		return nil
	}
	return errors.New("successful deposit receipt has no deposit event")
}

// DepositedAmount returns the amount, in shor, of the first deposit the
// contract has logged for publicKey since fromBlock, and false if there is none
// yet.
func (depositor *Depositor) DepositedAmount(ctx context.Context, publicKey []byte, fromBlock uint64) (uint64, bool, error) {
	logs, err := depositor.node.Execution.FilterLogs(ctx, qrl.FilterQuery{
		FromBlock: new(big.Int).SetUint64(fromBlock),
		Addresses: []common.Address{depositor.address},
	})
	if err != nil {
		return 0, false, fmt.Errorf("query deposit events: %w", err)
	}
	return depositor.findDeposit(logs, publicKey)
}

func (depositor *Depositor) findDeposit(logs []types.Log, publicKey []byte) (uint64, bool, error) {
	for _, log := range logs {
		var event depositEvent
		if err := depositor.contract.UnpackLog(&event, "DepositEvent", log); err != nil {
			return 0, false, fmt.Errorf("decode deposit event: %w", err)
		}
		if bytes.Equal(event.PublicKey, publicKey) {
			amount, err := event.amount()
			return amount, err == nil, err
		}
	}
	return 0, false, nil
}

func (event depositEvent) amount() (uint64, error) {
	if len(event.Amount) != 8 {
		return 0, fmt.Errorf("deposit event amount must be 8 bytes, got %d", len(event.Amount))
	}
	return binary.LittleEndian.Uint64(event.Amount), nil
}

func depositInput(
	key *Key,
	withdrawalRecipient common.Address,
	amount uint64,
	domain signing.Domain,
) (signing.DepositData, signing.Root, error) {
	message := signing.DepositMessage{
		PublicKey:           key.PublicKey(),
		WithdrawalRecipient: withdrawalRecipient.Bytes(),
		Amount:              amount,
		RandaoCommitment:    key.RandaoCommitment(),
	}
	messageRoot, err := message.HashTreeRoot()
	if err != nil {
		return signing.DepositData{}, signing.Root{}, err
	}

	signingRoot := signing.SigningRoot(messageRoot, domain)
	signature, err := key.Sign(signingRoot[:])
	if err != nil {
		return signing.DepositData{}, signing.Root{}, err
	}
	if err := signing.Verify(signingRoot, message.PublicKey, signature); err != nil {
		return signing.DepositData{}, signing.Root{}, fmt.Errorf("verify generated deposit signature: %w", err)
	}

	data := signing.DepositData{DepositMessage: message, Signature: signature}
	dataRoot, err := data.HashTreeRoot()
	return data, dataRoot, err
}
