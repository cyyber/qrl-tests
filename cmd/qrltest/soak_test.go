package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSoakRejectsExistingWithParams(t *testing.T) {
	paramsFile := filepath.Join(t.TempDir(), "params.yaml")
	require.NoError(t, os.WriteFile(paramsFile, []byte("custom: true\n"), 0o600))

	err := runCommandError(t, "soak", "--existing", "--params-file", paramsFile)
	require.ErrorContains(t, err, "custom parameters cannot be used with --existing")
}

func TestSoakRejectsNonPositiveDuration(t *testing.T) {
	err := runCommandError(t, "soak", "--duration", "0")
	require.ErrorContains(t, err, "duration must be positive")
}

func runCommandError(t *testing.T, arguments ...string) error {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := newApp(new(recordingController))
	app.Writer, app.ErrWriter = &stdout, &stderr
	return app.RunContext(t.Context(), append([]string{"qrltest"}, arguments...))
}
