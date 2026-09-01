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
	ConstructorAmount  string          `json:"constructorAmount"`
	ABI                json.RawMessage `json:"abi"`
	ConstructorInput   string          `json:"constructorInput"`
	ConstructorGas     uint64          `json:"constructorGas"`
	ConstructorTag     string          `json:"constructorTag"`
	ConstructorPayload string          `json:"constructorPayload"`
}

type constructorArguments struct {
	amount    *big.Int
	recipient common.Address
	tag       [33]byte
	payload   []byte
}

func prepareConstructorParameters(
	ctx context.Context,
	node *live.Node,
	abiJSON, bytecode []byte,
) ([]byte, error) {
	auth, err := newTransactor(ctx, node)
	if err != nil {
		return nil, err
	}
	arguments, err := constructorTestArguments(auth.From)
	if err != nil {
		return nil, err
	}
	parameters := constructorParameters{
		Recipient:          arguments.recipient.Hex(),
		Bytecode:           hexutil.Encode(bytecode),
		ConstructorAmount:  arguments.amount.String(),
		ABI:                abiJSON,
		ConstructorTag:     hexutil.Encode(arguments.tag[:]),
		ConstructorPayload: hexutil.Encode(arguments.payload),
	}
	if err := parameters.buildConstructorCase(ctx, node, auth.From, abiJSON, bytecode, arguments); err != nil {
		return nil, err
	}
	return json.Marshal(parameters)
}

func constructorTestArguments(recipient common.Address) (constructorArguments, error) {
	amount, err := parseStoreValue()
	if err != nil {
		return constructorArguments{}, err
	}
	tag := [33]byte{}
	for index := range tag {
		tag[index] = byte(index + 1)
	}
	payload := make([]byte, 65)
	for index := range payload {
		payload[index] = byte(0xff - index)
	}
	return constructorArguments{
		amount:    amount,
		recipient: recipient,
		tag:       tag,
		payload:   payload,
	}, nil
}

func (arguments constructorArguments) values() []any {
	return []any{arguments.amount, arguments.recipient, arguments.tag, arguments.payload}
}

func (parameters *constructorParameters) buildConstructorCase(
	ctx context.Context,
	node *live.Node,
	sender common.Address,
	abiJSON,
	bytecode []byte,
	arguments constructorArguments,
) error {
	contractABI, err := abi.JSON(bytes.NewReader(abiJSON))
	if err != nil {
		return fmt.Errorf("parse contract ABI: %w", err)
	}
	constructorSuffix, err := contractABI.Constructor.Inputs.Pack(arguments.values()...)
	if err != nil {
		return fmt.Errorf("pack constructor data: %w", err)
	}
	constructorInput := append(bytes.Clone(bytecode), constructorSuffix...)
	constructorGas, err := node.Execution.EstimateGas(ctx, qrl.CallMsg{
		From: sender,
		Data: constructorInput,
	})
	if err != nil {
		return fmt.Errorf("estimate constructor coverage gas: %w", err)
	}
	parameters.ConstructorInput = hexutil.Encode(constructorInput)
	parameters.ConstructorGas = constructorGas
	return nil
}
