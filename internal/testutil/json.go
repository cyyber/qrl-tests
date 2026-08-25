package testutil

import (
	"testing"

	"github.com/cyyber/qrl-tests/internal/jsonfile"
	"github.com/stretchr/testify/require"
)

// ReadJSON reads and decodes a JSON file into T.
func ReadJSON[T any](t testing.TB, path string) T {
	t.Helper()
	value, err := jsonfile.Read[T](path, "JSON file")
	require.NoError(t, err)
	return value
}
