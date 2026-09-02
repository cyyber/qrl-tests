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
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
)

const (
	vm64ValueDecimal = "6703903964971298549787012499102923063739682910296196688861780721860882015036773488400937149083451713845015929093243025426876941405973284973216824503046708"
	storeLabel       = "indexed dynamic label"
)

type deploymentParameters struct {
	Sender         string          `json:"sender"`
	TxHash         string          `json:"txHash"`
	RawTransaction string          `json:"rawTransaction"`
	ABI            json.RawMessage `json:"abi"`
}

type eventParameters struct {
	deploymentParameters
	StoreData    string `json:"storeData"`
	StoreValue   string `json:"storeValue"`
	StoreLabel   string `json:"storeLabel"`
	StorePayload string `json:"storePayload"`
}

type constructorArguments struct {
	amount    *big.Int
	recipient common.Address
	tag       [33]byte
	payload   []byte
}

func (arguments constructorArguments) values() []any {
	return []any{arguments.amount, arguments.recipient, arguments.tag, arguments.payload}
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

func prepareRawDeployment(
	auth *bind.TransactOpts,
	backend bind.ContractBackend,
	contractABI abi.ABI,
	abiJSON, bytecode []byte,
	arguments ...any,
) (deploymentParameters, error) {
	_, transaction, _, err := bind.DeployContract(
		auth,
		contractABI,
		bytecode,
		backend,
		arguments...,
	)
	if err != nil {
		return deploymentParameters{}, fmt.Errorf("prepare raw deployment transaction: %w", err)
	}
	rawTransaction, err := transaction.MarshalBinary()
	if err != nil {
		return deploymentParameters{}, fmt.Errorf("encode raw deployment transaction: %w", err)
	}
	return deploymentParameters{
		Sender:         auth.From.Hex(),
		TxHash:         transaction.Hash().Hex(),
		RawTransaction: hexutil.Encode(rawTransaction),
		ABI:            abiJSON,
	}, nil
}

func prepareConsoleProbeDeployment(
	ctx context.Context,
	node *live.Node,
	abiJSON, bytecode []byte,
) (deploymentParameters, abi.ABI, error) {
	contractABI, err := abi.JSON(bytes.NewReader(abiJSON))
	if err != nil {
		return deploymentParameters{}, abi.ABI{}, fmt.Errorf("parse contract ABI: %w", err)
	}
	auth, err := newTransactor(ctx, node)
	if err != nil {
		return deploymentParameters{}, abi.ABI{}, err
	}
	arguments := constructorArguments{
		amount:    new(big.Int),
		recipient: auth.From,
		payload:   []byte{},
	}

	deployment, err := prepareRawDeployment(
		auth,
		node.Execution,
		contractABI,
		abiJSON,
		bytecode,
		arguments.values()...,
	)
	if err != nil {
		return deploymentParameters{}, abi.ABI{}, err
	}
	return deployment, contractABI, nil
}

func prepareContractParameters(
	ctx context.Context,
	node *live.Node,
	abiJSON, bytecode []byte,
) ([]byte, error) {
	deployment, _, err := prepareConsoleProbeDeployment(ctx, node, abiJSON, bytecode)
	if err != nil {
		return nil, err
	}
	return json.Marshal(deployment)
}

func prepareEventParameters(
	ctx context.Context,
	node *live.Node,
	abiJSON, bytecode []byte,
) ([]byte, error) {
	deployment, contractABI, err := prepareConsoleProbeDeployment(ctx, node, abiJSON, bytecode)
	if err != nil {
		return nil, err
	}
	storeValue, err := parseVM64Value()
	if err != nil {
		return nil, err
	}
	storePayload := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}
	storeData, err := contractABI.Pack("store", storeValue, storeLabel, storePayload)
	if err != nil {
		return nil, fmt.Errorf("pack store call: %w", err)
	}
	parameters := eventParameters{
		deploymentParameters: deployment,
		StoreData:            hexutil.Encode(storeData),
		StoreValue:           vm64ValueDecimal,
		StoreLabel:           storeLabel,
		StorePayload:         hexutil.Encode(storePayload),
	}
	return json.Marshal(parameters)
}

func parseVM64Value() (*big.Int, error) {
	value, ok := new(big.Int).SetString(vm64ValueDecimal, 10)
	if !ok {
		return nil, errors.New("parse VM64 fixture value")
	}
	return value, nil
}
