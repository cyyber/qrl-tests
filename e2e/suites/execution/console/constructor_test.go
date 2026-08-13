package console

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	endtoendlive "github.com/cyyber/qrl-tests/e2e/internal/live"
	qrl "github.com/theQRL/go-qrl"
	"github.com/theQRL/go-qrl/accounts/abi"
	"github.com/theQRL/go-qrl/common/hexutil"
)

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

func (parameters *consoleParameters) buildConstructorCase(
	ctx context.Context,
	session *endtoendlive.Node,
	deployment preparedDeployment,
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
		deployment.auth.From,
		constructorTag,
		constructorPayload,
	)
	if err != nil {
		return fmt.Errorf("pack constructor coverage data: %w", err)
	}
	constructorInput := append(bytes.Clone(bytecode), constructorSuffix...)
	constructorGas, err := session.Execution.EstimateGas(ctx, qrl.CallMsg{
		From: deployment.auth.From,
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
