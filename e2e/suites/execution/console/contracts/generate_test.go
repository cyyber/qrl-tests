package contracts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/accounts/abi"
)

func TestConsoleProbeArtifacts(t *testing.T) {
	contractABI, err := abi.JSON(strings.NewReader(string(ConsoleProbeABI)))
	require.NoError(t, err)
	constructorTypes := make([]string, len(contractABI.Constructor.Inputs))
	for index, input := range contractABI.Constructor.Inputs {
		constructorTypes[index] = input.Type.String()
	}
	require.Equal(t, []string{"uint512", "address", "bytes33", "bytes"}, constructorTypes)
	for name, signature := range map[string]string{
		"constructorPayloadHash": "constructorPayloadHash()",
		"constructorRecipient":   "constructorRecipient()",
		"constructorTag":         "constructorTag()",
		"failTransaction":        "failTransaction()",
		"pay":                    "pay(uint512)",
		"store":                  "store(uint512,string,bytes)",
		"stored":                 "stored()",
	} {
		require.Equal(t, signature, contractABI.Methods[name].Sig)
	}
	require.Equal(t, "payable", contractABI.Methods["pay"].StateMutability)
	storedEvent, ok := contractABI.Events["Stored"]
	require.True(t, ok)
	require.Equal(t, "Stored(address,string,bytes,uint512)", storedEvent.Sig)
	storedIndexed := make([]bool, len(storedEvent.Inputs))
	for index, input := range storedEvent.Inputs {
		storedIndexed[index] = input.Indexed
	}
	require.Equal(t, []bool{true, true, true, false}, storedIndexed)

	bytecode, err := ConsoleProbeBytecode()
	require.NoError(t, err)
	require.NotEmpty(t, bytecode)
}
