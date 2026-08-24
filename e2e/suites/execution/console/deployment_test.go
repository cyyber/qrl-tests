package console

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/theQRL/go-qrl/accounts/abi"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
)

const (
	storeValueDecimal = "6703903964971298549787012499102923063739682910296196688861780721860882015036773488400937149083451713845015929093243025426876941405973284973216824503046708"
	storeLabel        = "indexed dynamic label"
)

type deploymentParameters struct {
	Sender         string          `json:"sender"`
	TxHash         string          `json:"txHash"`
	RawTransaction string          `json:"rawTransaction"`
	ABI            json.RawMessage `json:"abi"`
}

type eventParameters struct {
	deploymentParameters
	StoreTxHash  string `json:"storeTxHash"`
	StoreRaw     string `json:"storeRawTransaction"`
	StoreData    string `json:"storeData"`
	StoreValue   string `json:"storeValue"`
	StoreLabel   string `json:"storeLabel"`
	StorePayload string `json:"storePayload"`
}

type preparedDeployment struct {
	auth     *bind.TransactOpts
	contract *bind.BoundContract
	tx       *types.Transaction
	raw      []byte
}

func prepareContractParameters(
	ctx context.Context,
	node *live.Node,
	abiJSON, bytecode []byte,
) ([]byte, error) {
	deployment, err := prepareDeploymentTransaction(ctx, node, abiJSON, bytecode)
	if err != nil {
		return nil, err
	}
	return json.Marshal(deploymentParameters{
		Sender:         deployment.auth.From.Hex(),
		TxHash:         deployment.tx.Hash().Hex(),
		RawTransaction: hexutil.Encode(deployment.raw),
		ABI:            abiJSON,
	})
}

// prepareDeploymentTransaction builds and signs the creation transaction.
// The console scenario broadcasts its raw encoding.
func prepareDeploymentTransaction(
	ctx context.Context,
	node *live.Node,
	abiJSON, bytecode []byte,
) (preparedDeployment, error) {
	contractABI, err := abi.JSON(bytes.NewReader(abiJSON))
	if err != nil {
		return preparedDeployment{}, fmt.Errorf("parse contract ABI: %w", err)
	}
	auth, err := newTransactor(ctx, node)
	if err != nil {
		return preparedDeployment{}, err
	}

	_, tx, contract, err := bind.DeployContract(auth, contractABI, bytecode, node.Execution)
	if err != nil {
		return preparedDeployment{}, fmt.Errorf("prepare deployment transaction: %w", err)
	}
	raw, err := tx.MarshalBinary()
	if err != nil {
		return preparedDeployment{}, fmt.Errorf("encode deployment transaction: %w", err)
	}
	return preparedDeployment{auth: auth, contract: contract, tx: tx, raw: raw}, nil
}

func newTransactor(ctx context.Context, node *live.Node) (*bind.TransactOpts, error) {
	auth, err := bind.NewKeyedTransactorWithChainID(node.Wallet, node.ChainID)
	if err != nil {
		return nil, fmt.Errorf("create console transactor: %w", err)
	}
	auth.Context = ctx
	auth.NoSend = true
	return auth, nil
}

func prepareEventParameters(
	ctx context.Context,
	node *live.Node,
	abiJSON, bytecode []byte,
) ([]byte, error) {
	deployment, err := prepareDeploymentTransaction(ctx, node, abiJSON, bytecode)
	if err != nil {
		return nil, err
	}
	storeValue, err := parseStoreValue()
	if err != nil {
		return nil, err
	}
	parameters := eventParameters{
		deploymentParameters: deploymentParameters{
			Sender:         deployment.auth.From.Hex(),
			TxHash:         deployment.tx.Hash().Hex(),
			RawTransaction: hexutil.Encode(deployment.raw),
			ABI:            abiJSON,
		},
		StoreValue: storeValueDecimal,
		StoreLabel: storeLabel,
	}
	if err := parameters.buildStoreCase(deployment, storeValue); err != nil {
		return nil, err
	}
	return json.Marshal(parameters)
}

func parseStoreValue() (*big.Int, error) {
	value, ok := new(big.Int).SetString(storeValueDecimal, 10)
	if !ok {
		return nil, errors.New("parse store value")
	}
	return value, nil
}

func (parameters *eventParameters) buildStoreCase(
	deployment preparedDeployment,
	storeValue *big.Int,
) error {
	storePayload := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}
	auth := *deployment.auth
	auth.Nonce = new(big.Int).SetUint64(deployment.tx.Nonce() + 1)
	auth.GasLimit = 500_000
	storeTx, err := deployment.contract.Transact(&auth, "store", storeValue, storeLabel, storePayload)
	if err != nil {
		return fmt.Errorf("prepare store transaction: %w", err)
	}
	storeRaw, err := storeTx.MarshalBinary()
	if err != nil {
		return fmt.Errorf("encode store transaction: %w", err)
	}
	parameters.StoreTxHash = storeTx.Hash().Hex()
	parameters.StoreRaw = hexutil.Encode(storeRaw)
	parameters.StoreData = hexutil.Encode(storeTx.Data())
	parameters.StorePayload = hexutil.Encode(storePayload)
	return nil
}
