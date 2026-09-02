package console

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

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

func prepareConstructorParameters(
	ctx context.Context,
	node *live.Node,
	abiJSON, bytecode []byte,
) ([]byte, error) {
	recipient := common.Address(node.Wallet.GetAddress())
	arguments := constructorFixtureArguments(recipient)
	constructorInput, constructorGas, err := buildConstructorOracle(
		ctx,
		node,
		recipient,
		abiJSON,
		bytecode,
		arguments,
	)
	if err != nil {
		return nil, err
	}
	return json.Marshal(constructorParameters{
		Recipient:          arguments.recipient.Hex(),
		Bytecode:           hexutil.Encode(bytecode),
		ConstructorAmount:  arguments.amount.String(),
		ABI:                abiJSON,
		ConstructorInput:   constructorInput,
		ConstructorGas:     constructorGas,
		ConstructorTag:     hexutil.Encode(arguments.tag[:]),
		ConstructorPayload: hexutil.Encode(arguments.payload),
	})
}

func constructorFixtureArguments(recipient common.Address) constructorArguments {
	tag := [33]byte{}
	for index := range tag {
		tag[index] = byte(index + 1)
	}
	payload := make([]byte, 65)
	for index := range payload {
		payload[index] = byte(0xff - index)
	}
	return constructorArguments{
		amount:    fullWidthVM64Value(),
		recipient: recipient,
		tag:       tag,
		payload:   payload,
	}
}

func buildConstructorOracle(
	ctx context.Context,
	node *live.Node,
	sender common.Address,
	abiJSON, bytecode []byte,
	arguments constructorArguments,
) (string, uint64, error) {
	contractABI, err := abi.JSON(bytes.NewReader(abiJSON))
	if err != nil {
		return "", 0, fmt.Errorf("parse contract ABI: %w", err)
	}
	constructorSuffix, err := contractABI.Constructor.Inputs.Pack(arguments.abiValues()...)
	if err != nil {
		return "", 0, fmt.Errorf("pack constructor data: %w", err)
	}
	constructorInput := append(bytes.Clone(bytecode), constructorSuffix...)
	constructorGas, err := node.Execution.EstimateGas(ctx, qrl.CallMsg{
		From: sender,
		Data: constructorInput,
	})
	if err != nil {
		return "", 0, fmt.Errorf("estimate constructor gas: %w", err)
	}
	return hexutil.Encode(constructorInput), constructorGas, nil
}
