package contracts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/accounts/abi"
)

func TestConsoleProbe(t *testing.T) {
	contractABI, err := abi.JSON(strings.NewReader(string(ConsoleProbeABI)))
	require.NoError(t, err)
	constructorTypes := make([]string, len(contractABI.Constructor.Inputs))
	for index, input := range contractABI.Constructor.Inputs {
		constructorTypes[index] = input.Type.String()
	}
	require.Equal(t, []string{"uint512", "address", "bytes33", "bytes"}, constructorTypes)
	for _, method := range []string{
		"constructorPayloadHash",
		"constructorRecipient",
		"constructorTag",
		"failTransaction",
		"pay",
		"store",
		"stored",
	} {
		require.Contains(t, contractABI.Methods, method)
	}
	storedEvent, ok := contractABI.Events["Stored"]
	require.True(t, ok)
	storedTypes := make([]string, len(storedEvent.Inputs))
	storedIndexed := make([]bool, len(storedEvent.Inputs))
	for index, input := range storedEvent.Inputs {
		storedTypes[index] = input.Type.String()
		storedIndexed[index] = input.Indexed
	}
	require.Equal(t, []string{"address", "string", "bytes", "uint512"}, storedTypes)
	require.Equal(t, []bool{true, true, true, false}, storedIndexed)

	bytecode, err := ConsoleProbeBytecode()
	require.NoError(t, err)
	require.NotEmpty(t, bytecode)
}
