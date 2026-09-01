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
		"stored",
	} {
		require.Contains(t, contractABI.Methods, method)
	}

	bytecode, err := ConsoleProbeBytecode()
	require.NoError(t, err)
	require.NotEmpty(t, bytecode)
}
