package consolefixture

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/accounts/abi"
)

func TestGeneratedArtifacts(t *testing.T) {
	_, err := abi.JSON(strings.NewReader(string(ABI)))
	require.NoError(t, err)

	bytecode, err := Bytecode()
	require.NoError(t, err)
	require.NotEmpty(t, bytecode)
}
