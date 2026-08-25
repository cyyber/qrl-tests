package jsonfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type testDocument struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestReadWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "document.json")
	want := testDocument{Name: "example", Count: 1}

	require.NoError(t, Write(path, want, "test document"))
	got, err := Read[testDocument](path, "test document")
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestReadErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")
	value, err := Read[testDocument](missing, "test document")
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Contains(t, err.Error(), missing)
	require.Zero(t, value)

	malformed := filepath.Join(t.TempDir(), "malformed.json")
	require.NoError(t, os.WriteFile(malformed, []byte(`{"name":"partial","count":"invalid"}`), 0o600))
	value, err = Read[testDocument](malformed, "test document")
	require.ErrorContains(t, err, "decode test document")
	require.Contains(t, err.Error(), malformed)
	require.Zero(t, value)
}
