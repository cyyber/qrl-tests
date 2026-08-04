// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

package fixture

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/common"
)

func TestDevelopmentWalletAddress(t *testing.T) {
	wallet, err := DevelopmentWallet()
	require.NoError(t, err)
	require.Equal(t, DevelopmentWalletAddress, common.Address(wallet.GetAddress()).Hex())
}
