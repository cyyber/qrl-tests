package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/accounts/keystore"
)

func TestClefArgsCreatesRuntimeAccount(t *testing.T) {
	args, password, cleanup, err := clefArgs([]string{"--keystore=/mounted", "--http"})
	require.NoError(t, err)
	t.Cleanup(cleanup)
	require.NotEmpty(t, password)
	require.Equal(t, "--stdio-ui", args[len(args)-1])
	require.Contains(t, args, "--http")
	var keystorePath string
	for _, arg := range args {
		if strings.HasPrefix(arg, "--keystore=") && arg != "--keystore=/mounted" {
			keystorePath = strings.TrimPrefix(arg, "--keystore=")
		}
	}
	require.NotEmpty(t, keystorePath)
	store := keystore.NewKeyStore(
		keystorePath,
		keystore.LightArgon2idT,
		keystore.LightArgon2idM,
		keystore.LightArgon2idP,
	)
	require.Len(t, store.Accounts(), 1)
}
