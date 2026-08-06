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

	_, err = ParseBackend("unknown")
	require.Error(t, err)
}

func TestImagesWithDefaults(t *testing.T) {
	images := Images{Execution: " registry.example/go-qrl:test ", Clef: "  "}.withDefaults()
	require.Equal(t, "registry.example/go-qrl:test", images.Execution)
	require.Equal(t, DefaultClefImage, images.Clef)
	require.Equal(t, DefaultImages(), Images{}.withDefaults())
}
