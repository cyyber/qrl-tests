package console

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/theQRL/go-qrl/accounts/abi"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
)

const storeValueDecimal = "6703903964971298549787012499102923063739682910296196688861780721860882015036773488400937149083451713845015929093243025426876941405973284973216824503046708"

type consoleParameters struct {
	Address             string          `json:"address"`
	TxHash              string          `json:"txHash"`
	RawTransaction      string          `json:"rawTransaction"`
	StoreValue          string          `json:"storeValue"`
	ABI                 json.RawMessage `json:"abi"`
	IndexedABI          json.RawMessage `json:"indexedABI"`
	IndexedTxHash       string          `json:"indexedTxHash"`
	IndexedRaw          string          `json:"indexedRawTransaction"`
	IndexedDelta        string          `json:"indexedDelta"`
	IndexedAmount       string          `json:"indexedAmount"`
	IndexedCode         string          `json:"indexedCode"`
	IndexedLabel        string          `json:"indexedLabel"`
	IndexedLabelTopic   string          `json:"indexedLabelTopic"`
	IndexedPayload      string          `json:"indexedPayload"`
	IndexedPayloadTopic string          `json:"indexedPayloadTopic"`
	NumberTopics        []string        `json:"numberTopics"`
	BytesTopics         []string        `json:"bytesTopics"`
	ReferenceTopics     []string        `json:"referenceTopics"`
}

type preparedDeployment struct {
	auth *bind.TransactOpts
	tx   *types.Transaction
	raw  []byte
}

func prepareParameters(
	ctx context.Context,
	session *live.Node,
	abiJSON, bytecode []byte,
) ([]byte, error) {
	deployment, err := prepareDeployment(ctx, session, abiJSON, bytecode)
	if err != nil {
		return nil, err
	}
	parameters := consoleParameters{
		Address:        deployment.auth.From.Hex(),
		TxHash:         deployment.tx.Hash().Hex(),
		RawTransaction: hexutil.Encode(deployment.raw),
		StoreValue:     storeValueDecimal,
		ABI:            abiJSON,
	}
	if err := parameters.buildIndexedEventCase(session, deployment); err != nil {
		return nil, err
	}
	parameterJSON, err := json.Marshal(parameters)
	if err != nil {
		return nil, fmt.Errorf("encode console parameters: %w", err)
	}
	return parameterJSON, nil
}

func prepareDeployment(
	ctx context.Context,
	session *live.Node,
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

	_, tx, _, err := bind.DeployContract(auth, contractABI, bytecode, session.Execution)
	if err != nil {
		return preparedDeployment{}, fmt.Errorf("prepare deployment transaction: %w", err)
	}
	raw, err := tx.MarshalBinary()
	if err != nil {
		return preparedDeployment{}, fmt.Errorf("encode deployment transaction: %w", err)
	}
	return preparedDeployment{auth: auth, tx: tx, raw: raw}, nil
}
