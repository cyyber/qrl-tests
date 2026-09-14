package validatorops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/cyyber/qrl-tests/e2e/internal/consensuscontext"
	"github.com/cyyber/qrl-tests/e2e/internal/consensuscrypto"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/theQRL/go-qrl/accounts/abi"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/params"
)

// depositContractABI is the QRL deposit contract surface: the Ethereum
// contract with ML-DSA-87 keys, 64-byte withdrawal recipients, and the
// validator's RANDAO commitment.
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

// Depositor submits deposits from the development wallet, which also serves
// as the withdrawal recipient of every validator it funds.
type Depositor struct {
	node     *live.Node
	contract *bind.BoundContract
	address  common.Address
	domain   [consensuscrypto.RootLength]byte
}

func NewDepositor(ctx context.Context, node *live.Node, chain consensuscontext.Context) (*Depositor, error) {
	config, err := node.Beacon.DepositContract(ctx)
	if err != nil {
		return nil, err
	}
	address, err := common.NewAddressFromString(config.Address)
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

// Deposit stakes amountInShor for key and waits for the transaction to be
// mined with a matching DepositEvent.
func (depositor *Depositor) Deposit(ctx context.Context, key *Key, amountInShor uint64) (*types.Receipt, error) {
	data, root, err := depositInput(key, depositor.node.Address, amountInShor, depositor.domain)
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

func (depositor *Depositor) verifyEvent(receipt *types.Receipt, data consensuscrypto.DepositData) error {
	for _, log := range receipt.Logs {
		if log.Address != depositor.address {
			continue
		}
		var event depositEvent
		if err := depositor.contract.UnpackLog(&event, "DepositEvent", *log); err != nil {
			return fmt.Errorf("decode deposit event: %w", err)
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

func depositInput(
	key *Key,
	withdrawalRecipient common.Address,
	amount uint64,
	domain [consensuscrypto.RootLength]byte,
) (consensuscrypto.DepositData, [consensuscrypto.RootLength]byte, error) {
	message := consensuscrypto.DepositMessage{
		PublicKey:           key.PublicKey(),
		WithdrawalRecipient: withdrawalRecipient.Bytes(),
		Amount:              amount,
		RandaoCommitment:    key.RandaoCommitment(),
	}
	messageRoot, err := message.HashTreeRoot()
	if err != nil {
		return consensuscrypto.DepositData{}, [consensuscrypto.RootLength]byte{}, err
	}

	signingRoot := consensuscrypto.SigningRoot(messageRoot, domain)
	signature, err := key.Sign(signingRoot[:])
	if err != nil {
		return consensuscrypto.DepositData{}, [consensuscrypto.RootLength]byte{}, err
	}
	if err := consensuscrypto.Verify(signingRoot, message.PublicKey, signature); err != nil {
		return consensuscrypto.DepositData{}, [consensuscrypto.RootLength]byte{}, fmt.Errorf("verify generated deposit signature: %w", err)
	}

	data := consensuscrypto.DepositData{
		PublicKey:           message.PublicKey,
		WithdrawalRecipient: message.WithdrawalRecipient,
		Amount:              message.Amount,
		RandaoCommitment:    message.RandaoCommitment,
		Signature:           signature,
	}
	dataRoot, err := data.HashTreeRoot()
	return data, dataRoot, err
}
