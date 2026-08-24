package console

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/cyyber/qrl-tests/e2e/internal/live"
	qrl "github.com/theQRL/go-qrl"
	"github.com/theQRL/go-qrl/accounts/abi"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
)

type constructorParameters struct {
	Recipient          string          `json:"recipient"`
	Bytecode           string          `json:"bytecode"`
	StoreValue         string          `json:"storeValue"`
	ABI                json.RawMessage `json:"abi"`
	ConstructorABI     json.RawMessage `json:"constructorABI"`
	ConstructorInput   string          `json:"constructorInput"`
	ConstructorGas     uint64          `json:"constructorGas"`
	ConstructorTag     string          `json:"constructorTag"`
	ConstructorPayload string          `json:"constructorPayload"`
}

const constructorABIJSON = `[{
  "inputs":[
    {"name":"amount","type":"uint512"},
    {"name":"recipient","type":"address"},
    {"name":"tag","type":"bytes33"},
    {"name":"payload","type":"bytes"}
  ],
  "stateMutability":"nonpayable",
  "type":"constructor"
}]`

func prepareConstructorParameters(
	ctx context.Context,
	node *live.Node,
	abiJSON, bytecode []byte,
) ([]byte, error) {
	auth, err := newTransactor(ctx, node)
	if err != nil {
		return nil, err
	}
	storeValue, err := parseStoreValue()
	if err != nil {
		return nil, err
	}
	parameters := constructorParameters{
		Recipient:  auth.From.Hex(),
		Bytecode:   hexutil.Encode(bytecode),
		StoreValue: storeValueDecimal,
		ABI:        abiJSON,
	}
	if err := parameters.buildConstructorCase(ctx, node, auth.From, bytecode, storeValue); err != nil {
		return nil, err
	}
	return json.Marshal(parameters)
}

func (parameters *constructorParameters) buildConstructorCase(
	ctx context.Context,
	node *live.Node,
	sender common.Address,
	bytecode []byte,
	storeValue *big.Int,
) error {
	constructorABI, err := abi.JSON(bytes.NewBufferString(constructorABIJSON))
	if err != nil {
		return fmt.Errorf("parse constructor coverage ABI: %w", err)
	}
	constructorTag := [33]byte{}
	for index := range constructorTag {
		constructorTag[index] = byte(index + 1)
	}
	constructorPayload := make([]byte, 65)
	for index := range constructorPayload {
		constructorPayload[index] = byte(0xff - index)
	}
	constructorSuffix, err := constructorABI.Constructor.Inputs.Pack(
		storeValue,
		sender,
		constructorTag,
		constructorPayload,
	)
	if err != nil {
		return fmt.Errorf("pack constructor coverage data: %w", err)
	}
	constructorInput := append(bytes.Clone(bytecode), constructorSuffix...)
	constructorGas, err := node.Execution.EstimateGas(ctx, qrl.CallMsg{
		From: sender,
		Data: constructorInput,
	})
	if err != nil {
		return fmt.Errorf("estimate constructor coverage gas: %w", err)
	}
	parameters.ConstructorABI = json.RawMessage(constructorABIJSON)
	parameters.ConstructorInput = hexutil.Encode(constructorInput)
	parameters.ConstructorGas = constructorGas
	parameters.ConstructorTag = hexutil.Encode(constructorTag[:])
	parameters.ConstructorPayload = hexutil.Encode(constructorPayload)
	return nil
}
