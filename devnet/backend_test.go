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

func TestImagesValidate(t *testing.T) {
	require.NoError(t, DefaultImages().validate())

	images := DefaultImages()
	images.Execution = " "
	require.ErrorContains(t, images.validate(), "execution image is empty")
}
