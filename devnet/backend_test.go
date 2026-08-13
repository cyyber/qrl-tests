package devnet

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseBackend(t *testing.T) {
	backend, err := ParseBackend("")
	require.NoError(t, err)
	require.Equal(t, BackendDocker, backend)

	backend, err = ParseBackend("kubernetes")
	require.NoError(t, err)
	require.Equal(t, BackendKubernetes, backend)

	backend, err = ParseBackend("  docker  ")
	require.NoError(t, err)
	require.Equal(t, BackendDocker, backend)

	backend, err = ParseBackend("   ")
	require.NoError(t, err)
	require.Equal(t, BackendDocker, backend)

	_, err = ParseBackend("unknown")
	require.Error(t, err)
}
