package console

import (
	"bytes"
	"context"
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

func prepareTopicParameters(ctx context.Context, node *live.Node) ([]byte, error) {
	indexedABI, err := abi.JSON(bytes.NewBufferString(indexedEventABIJSON))
	if err != nil {
		return nil, fmt.Errorf("parse indexed-event coverage ABI: %w", err)
	}
	auth, err := newTransactor(ctx, node)
	if err != nil {
		return nil, err
	}
	indexedDelta := big.NewInt(-1)
	indexedAmount := new(big.Int).Lsh(big.NewInt(1), 400)
	indexedAmount.Add(indexedAmount, big.NewInt(17))
	indexedCode := [33]byte{}
	for index := range indexedCode {
		indexedCode[index] = byte(0xa0 + index)
	}
	numberEvent := indexedABI.Events["IndexedNumbers"]
	flagTopic, err := packSingleWordIndexedTopic(numberEvent.Inputs[0], true)
	if err != nil {
		return nil, err
	}
	deltaTopic, err := packSingleWordIndexedTopic(numberEvent.Inputs[1], indexedDelta)
	if err != nil {
		return nil, err
	}
	amountTopic, err := packSingleWordIndexedTopic(numberEvent.Inputs[2], indexedAmount)
	if err != nil {
		return nil, err
	}
	bytesEvent := indexedABI.Events["IndexedBytes"]
	codeTopic, err := packSingleWordIndexedTopic(bytesEvent.Inputs[0], indexedCode)
	if err != nil {
		return nil, err
	}
	const indexedLabel = "VM64 indexed dynamic value"
	indexedPayload := []byte{0xab, 0xcd}
	referenceEvent := indexedABI.Events["IndexedReference"]
	accountTopic := common.AddressToLogTopic(auth.From)
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

	auth.GasLimit = 500_000
	_, indexedTx, _, err := bind.DeployContract(
		auth,
		indexedABI,
		indexedEventInitCode(numberTopics, bytesTopics, referenceTopics),
		node.Execution,
	)
	if err != nil {
		return nil, fmt.Errorf("prepare indexed-event transaction: %w", err)
	}
	indexedRaw, err := indexedTx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("encode indexed-event transaction: %w", err)
	}
	return json.Marshal(topicParameters{
		deploymentParameters: deploymentParameters{
			Sender:         auth.From.Hex(),
			ABI:            json.RawMessage(indexedEventABIJSON),
			TxHash:         indexedTx.Hash().Hex(),
			RawTransaction: hexutil.Encode(indexedRaw),
		},
		IndexedDelta:        indexedDelta.String(),
		IndexedAmount:       indexedAmount.String(),
		IndexedCode:         hexutil.Encode(indexedCode[:]),
		IndexedLabel:        indexedLabel,
		IndexedLabelTopic:   labelTopic.Hex(),
		IndexedPayload:      hexutil.Encode(indexedPayload),
		IndexedPayloadTopic: payloadTopic.Hex(),
		NumberTopics:        topicStrings(numberTopics),
		BytesTopics:         topicStrings(bytesTopics),
		ReferenceTopics:     topicStrings(referenceTopics),
	})
}

func packSingleWordIndexedTopic(argument abi.Argument, value any) (common.LogTopic, error) {
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
		// LOGn pops topics in declaration order, so push them in reverse.
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
