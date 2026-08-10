package devwallet

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/common"
)

func TestAddress(t *testing.T) {
	wallet, err := Restore()
	require.NoError(t, err)
	require.Equal(t, Address, common.Address(wallet.GetAddress()).Hex())
}
