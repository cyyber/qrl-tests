package console

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/theQRL/go-qrl/accounts/abi"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/vm"
	"github.com/theQRL/go-qrl/crypto"
)

const indexedEventABIJSON = `[{
  "anonymous":false,
  "inputs":[
    {"indexed":true,"name":"flag","type":"bool"},
    {"indexed":true,"name":"delta","type":"int512"},
    {"indexed":true,"name":"amount","type":"uint512"}
  ],
  "name":"IndexedNumbers",
  "type":"event"
},{
  "anonymous":false,
  "inputs":[{"indexed":true,"name":"code","type":"bytes33"}],
  "name":"IndexedBytes",
  "type":"event"
},{
  "anonymous":false,
  "inputs":[
    {"indexed":true,"name":"account","type":"address"},
    {"indexed":true,"name":"label","type":"string"},
    {"indexed":true,"name":"payload","type":"bytes"}
  ],
  "name":"IndexedReference",
  "type":"event"
}]`

func (parameters *consoleParameters) buildIndexedEventCase(
	session *live.Node,
	deployment preparedDeployment,
) error {
	indexedABI, err := abi.JSON(bytes.NewBufferString(indexedEventABIJSON))
	if err != nil {
		return fmt.Errorf("parse indexed-event coverage ABI: %w", err)
	}
	indexedDelta := big.NewInt(-1)
	indexedAmount := new(big.Int).Lsh(big.NewInt(1), 400)
	indexedAmount.Add(indexedAmount, big.NewInt(17))
	indexedCode := [33]byte{}
	for index := range indexedCode {
		indexedCode[index] = byte(0xa0 + index)
	}
	numberEvent := indexedABI.Events["IndexedNumbers"]
	flagTopic, err := packEventTopic(numberEvent.Inputs[0], true)
	if err != nil {
		return err
	}
	deltaTopic, err := packEventTopic(numberEvent.Inputs[1], indexedDelta)
	if err != nil {
		return err
	}
	amountTopic, err := packEventTopic(numberEvent.Inputs[2], indexedAmount)
	if err != nil {
		return err
	}
	bytesEvent := indexedABI.Events["IndexedBytes"]
	codeTopic, err := packEventTopic(bytesEvent.Inputs[0], indexedCode)
	if err != nil {
		return err
	}
	const indexedLabel = "VM64 indexed dynamic value"
	indexedPayload := []byte{0xab, 0xcd}
	referenceEvent := indexedABI.Events["IndexedReference"]
	accountTopic := common.AddressToLogTopic(deployment.auth.From)
	labelTopic := common.HashToLogTopic(crypto.Keccak256Hash([]byte(indexedLabel)))
	payloadTopic := common.HashToLogTopic(crypto.Keccak256Hash(indexedPayload))
	numberTopics := []common.LogTopic{
		common.HashToLogTopic(numberEvent.ID),
		flagTopic,
		deltaTopic,
		amountTopic,
	}
	bytesTopics := []common.LogTopic{common.HashToLogTopic(bytesEvent.ID), codeTopic}
	referenceTopics := []common.LogTopic{
		common.HashToLogTopic(referenceEvent.ID),
		accountTopic,
		labelTopic,
		payloadTopic,
	}

	auth := *deployment.auth
	auth.Nonce = new(big.Int).SetUint64(deployment.tx.Nonce() + 1)
	auth.GasLimit = 500_000
	_, indexedTx, _, err := bind.DeployContract(
		&auth,
		indexedABI,
		indexedEventInitCode(numberTopics, bytesTopics, referenceTopics),
		session.Execution,
	)
	if err != nil {
		return fmt.Errorf("prepare indexed-event transaction: %w", err)
	}
	indexedRaw, err := indexedTx.MarshalBinary()
	if err != nil {
		return fmt.Errorf("encode indexed-event transaction: %w", err)
	}
	parameters.IndexedABI = json.RawMessage(indexedEventABIJSON)
	parameters.IndexedTxHash = indexedTx.Hash().Hex()
	parameters.IndexedRaw = hexutil.Encode(indexedRaw)
	parameters.IndexedDelta = indexedDelta.String()
	parameters.IndexedAmount = indexedAmount.String()
	parameters.IndexedCode = hexutil.Encode(indexedCode[:])
	parameters.IndexedLabel = indexedLabel
	parameters.IndexedLabelTopic = labelTopic.Hex()
	parameters.IndexedPayload = hexutil.Encode(indexedPayload)
	parameters.IndexedPayloadTopic = payloadTopic.Hex()
	parameters.NumberTopics = topicStrings(numberTopics)
	parameters.BytesTopics = topicStrings(bytesTopics)
	parameters.ReferenceTopics = topicStrings(referenceTopics)
	return nil
}

func packEventTopic(argument abi.Argument, value any) (common.LogTopic, error) {
	encoded, err := (abi.Arguments{argument}).Pack(value)
	if err != nil {
		return common.LogTopic{}, fmt.Errorf("pack indexed %s topic: %w", argument.Type, err)
	}
	if len(encoded) != common.LogTopicLength {
		return common.LogTopic{}, fmt.Errorf(
			"pack indexed %s topic: got %d bytes",
			argument.Type,
			len(encoded),
		)
	}
	var topic common.LogTopic
	copy(topic[:], encoded)
	return topic, nil
}

func indexedEventInitCode(events ...[]common.LogTopic) []byte {
	var code []byte
	for _, topics := range events {
		for index := len(topics) - 1; index >= 0; index-- {
			code = append(code, byte(vm.PUSH64))
			code = append(code, topics[index][:]...)
		}
		code = append(
			code,
			byte(vm.PUSH1), 0,
			byte(vm.PUSH1), 0,
			byte(vm.LOG0)+byte(len(topics)),
		)
	}
	return append(code,
		byte(vm.PUSH1), 0,
		byte(vm.PUSH1), 0,
		byte(vm.RETURN),
	)
}

func topicStrings(topics []common.LogTopic) []string {
	encoded := make([]string, len(topics))
	for index, topic := range topics {
		encoded[index] = topic.Hex()
	}
	return encoded
}
