package live

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntimeGQRL(t *testing.T) {
	runtime := &Runtime{gqrl: "/tmp/gqrl"}
	path, err := runtime.GQRL()
	require.NoError(t, err)
	require.Equal(t, "/tmp/gqrl", path)

	_, err = new(Runtime).GQRL()
	require.ErrorContains(t, err, "not configured")
}
