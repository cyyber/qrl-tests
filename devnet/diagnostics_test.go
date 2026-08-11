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
		if strings.HasPrefix(command, "kurtosis enclave dump") {
			return "dump progress", nil
		}
		if strings.Contains(command, "--quiet") {
			return "aaa111\nbbb222\n", nil
		}
		return "captured " + name, nil
	}

	require.NoError(t, collectDiagnostics(t.Context(), run, BackendDocker, "qrl-tests-abi", output))

	const kurtosisManaged = "--filter label=com.kurtosistech.app-id=kurtosis"
	require.Equal(t, []string{
		"kurtosis enclave inspect qrl-tests-abi",
		"kurtosis enclave dump qrl-tests-abi " + filepath.Join(output, "kurtosis", "dump"),
		"docker ps --all --no-trunc " + kurtosisManaged,
		"docker ps --all --quiet " + kurtosisManaged,
		"docker stats --no-stream aaa111 bbb222",
	}, commands)

	inspect, err := os.ReadFile(filepath.Join(output, "kurtosis", "inspect.txt"))
	require.NoError(t, err)
	require.Equal(t, "captured kurtosis", string(inspect))
	containers, err := os.ReadFile(filepath.Join(output, "runtime", "containers.txt"))
	require.NoError(t, err)
	require.Equal(t, "captured docker", string(containers))
	require.FileExists(t, filepath.Join(output, "runtime", "stats.txt"))
}

func TestCollectDiagnosticsSkipsDockerOnKubernetes(t *testing.T) {
	output := t.TempDir()
	var commands []string
	run := func(_ context.Context, name string, arguments ...string) (string, error) {
		commands = append(commands, name+" "+strings.Join(arguments, " "))
		return "", nil
	}

	require.NoError(t, collectDiagnostics(t.Context(), run, BackendKubernetes, "qrl-tests-abi", output))
	for _, command := range commands {
		require.NotContains(t, command, "docker")
	}
	require.NoDirExists(t, filepath.Join(output, "runtime"))
}

func TestCollectDiagnosticsKeepsGoingOnFailures(t *testing.T) {
	output := t.TempDir()
	run := func(_ context.Context, name string, arguments ...string) (string, error) {
		if name == "kurtosis" && arguments[1] == "dump" {
			return "", errors.New("enclave gone")
		}
		return "captured", nil
	}

	err := collectDiagnostics(t.Context(), run, BackendDocker, "qrl-tests-abi", output)
	require.ErrorContains(t, err, "kurtosis enclave dump")
	require.FileExists(t, filepath.Join(output, "kurtosis", "inspect.txt"),
		"a failing step must not stop the remaining captures")
	require.FileExists(t, filepath.Join(output, "runtime", "containers.txt"))
}
