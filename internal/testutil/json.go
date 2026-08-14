package testutil

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// ReadJSON reads and decodes a JSON file into T.
func ReadJSON[T any](t testing.TB, path string) T {
	t.Helper()
	payload, err := os.ReadFile(path)
	require.NoError(t, err)
	var value T
	require.NoError(t, json.Unmarshal(payload, &value))
	return value
}
