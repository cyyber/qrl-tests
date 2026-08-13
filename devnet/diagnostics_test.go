package devnet

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollectDiagnostics(t *testing.T) {
	output := t.TempDir()
	var commands []string
	run := func(_ context.Context, name string, arguments ...string) (string, error) {
		command := name + " " + strings.Join(arguments, " ")
		commands = append(commands, command)
		return "captured " + name, nil
	}

	require.NoError(t, collectDiagnostics(t.Context(), run, "qrl-tests-abi", output))

	require.Equal(t, []string{
		"kurtosis enclave inspect qrl-tests-abi",
		"kurtosis service logs --all-services --all qrl-tests-abi",
	}, commands)

	inspect, err := os.ReadFile(filepath.Join(output, "inspect.txt"))
	require.NoError(t, err)
	require.Equal(t, "captured kurtosis", string(inspect))
	logs, err := os.ReadFile(filepath.Join(output, "services.log"))
	require.NoError(t, err)
	require.Equal(t, "captured kurtosis", string(logs))
}

func TestCollectDiagnosticsKeepsGoingOnFailures(t *testing.T) {
	output := t.TempDir()
	run := func(_ context.Context, name string, arguments ...string) (string, error) {
		if name == "kurtosis" && arguments[1] == "logs" {
			return "", errors.New("logs unavailable")
		}
		return "captured", nil
	}

	err := collectDiagnostics(t.Context(), run, "qrl-tests-abi", output)
	require.ErrorContains(t, err, "kurtosis service logs")
	require.FileExists(t, filepath.Join(output, "inspect.txt"),
		"a failing step must not stop the remaining captures")
}
