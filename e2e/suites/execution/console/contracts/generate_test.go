package contracts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/accounts/abi"
)

func TestConsoleProbe(t *testing.T) {
	_, err := abi.JSON(strings.NewReader(string(ConsoleProbeABI)))
	require.NoError(t, err)

	bytecode, err := ConsoleProbeBytecode()
	require.NoError(t, err)
	require.NotEmpty(t, bytecode)
}
