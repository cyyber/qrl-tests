package console

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	endtoendlive "github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/theQRL/go-qrl/accounts/abi"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
)

const (
	storeValueDecimal = "6703903964971298549787012499102923063739682910296196688861780721860882015036773488400937149083451713845015929093243025426876941405973284973216824503046708"
	storeLabel        = "indexed dynamic label"
)

type consoleParameters struct {
	Address            string          `json:"address"`
	Bytecode           string          `json:"bytecode"`
	TxHash             string          `json:"txHash"`
	RawTransaction     string          `json:"rawTransaction"`
	StoreTxHash        string          `json:"storeTxHash"`
	StoreRaw           string          `json:"storeRawTransaction"`
	StoreData          string          `json:"storeData"`
	StoreValue         string          `json:"storeValue"`
	StoreLabel         string          `json:"storeLabel"`
	StorePayload       string          `json:"storePayload"`
	ABI                json.RawMessage `json:"abi"`
	ConstructorABI     json.RawMessage `json:"constructorABI"`
	ConstructorInput   string          `json:"constructorInput"`
	ConstructorGas     uint64          `json:"constructorGas"`
	ConstructorTag     string          `json:"constructorTag"`
	ConstructorPayload string          `json:"constructorPayload"`
	IndexedABI         json.RawMessage `json:"indexedABI"`
	IndexedTxHash      string          `json:"indexedTxHash"`
	IndexedRaw         string          `json:"indexedRawTransaction"`
	IndexedDelta       string          `json:"indexedDelta"`
	IndexedAmount      string          `json:"indexedAmount"`
	IndexedCode        string          `json:"indexedCode"`
	NumberTopics       []string        `json:"numberTopics"`
	BytesTopics        []string        `json:"bytesTopics"`
}

type preparedDeployment struct {
	auth     *bind.TransactOpts
	contract *bind.BoundContract
	tx       *types.Transaction
	raw      []byte
}

func deploymentParameters(
	ctx context.Context,
	session *endtoendlive.Node,
	abiJSON, bytecode []byte,
) ([]byte, error) {
	deployment, err := buildDeployment(ctx, session, abiJSON, bytecode)
	if err != nil {
		return nil, err
	}
	storeValue, ok := new(big.Int).SetString(storeValueDecimal, 10)
	if !ok {
		return nil, errors.New("parse store value")
	}
	parameters := consoleParameters{
		Address:        deployment.auth.From.Hex(),
		Bytecode:       hexutil.Encode(bytecode),
		TxHash:         deployment.tx.Hash().Hex(),
		RawTransaction: hexutil.Encode(deployment.raw),
		StoreValue:     storeValueDecimal,
		StoreLabel:     storeLabel,
		ABI:            abiJSON,
	}
	if err := parameters.buildConstructorCase(ctx, session, deployment, bytecode, storeValue); err != nil {
		return nil, err
	}
	if err := parameters.buildIndexedEventCase(session, deployment); err != nil {
		return nil, err
	}
	if err := parameters.buildStoreCase(deployment, storeValue); err != nil {
		return nil, err
	}
	return json.Marshal(parameters)
}

func buildDeployment(
	ctx context.Context,
	session *endtoendlive.Node,
	abiJSON, bytecode []byte,
) (preparedDeployment, error) {
	contractABI, err := abi.JSON(bytes.NewReader(abiJSON))
	if err != nil {
		return preparedDeployment{}, fmt.Errorf("parse contract ABI: %w", err)
	}
	auth, err := bind.NewKeyedTransactorWithChainID(session.Wallet, session.ChainID)
	if err != nil {
		return preparedDeployment{}, fmt.Errorf("create deployment transactor: %w", err)
	}
	auth.Context = ctx
	auth.NoSend = true

	_, tx, contract, err := bind.DeployContract(auth, contractABI, bytecode, session.Execution)
	if err != nil {
		return preparedDeployment{}, fmt.Errorf("prepare deployment transaction: %w", err)
	}
	raw, err := tx.MarshalBinary()
	if err != nil {
		return preparedDeployment{}, fmt.Errorf("encode deployment transaction: %w", err)
	}
	return preparedDeployment{auth: auth, contract: contract, tx: tx, raw: raw}, nil
}

func (parameters *consoleParameters) buildStoreCase(
	deployment preparedDeployment,
	storeValue *big.Int,
) error {
	storePayload := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}
	auth := *deployment.auth
	auth.Nonce = new(big.Int).SetUint64(deployment.tx.Nonce() + 2)
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
