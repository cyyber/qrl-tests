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
		if command == "kurtosis enclave inspect qrl-tests-abi" {
			return strings.Join([]string{
				"UUID Name Status",
				"111 run-generate-genesis STOPPED",
				"222 el-1-gqrl-qrysm RUNNING",
				"333 cl-1-qrysm-gqrl RUNNING",
				"444 signer-clef RUNNING",
				"555 vc-1-gqrl-qrysm RUNNING",
			}, "\n"), nil
		}
		return "captured " + name, nil
	}

	require.NoError(t, collectDiagnostics(t.Context(), run, "qrl-tests-abi", output))

	require.Equal(t, []string{
		"kurtosis enclave inspect qrl-tests-abi",
		"kurtosis service logs --num 200 qrl-tests-abi el-1-gqrl-qrysm cl-1-qrysm-gqrl signer-clef vc-1-gqrl-qrysm",
	}, commands)

	inspect, err := os.ReadFile(filepath.Join(output, "inspect.txt"))
	require.NoError(t, err)
	require.Contains(t, string(inspect), "run-generate-genesis")
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
		return "111 el-1-gqrl-qrysm RUNNING", nil
	}

	err := collectDiagnostics(t.Context(), run, "qrl-tests-abi", output)
	require.ErrorContains(t, err, "kurtosis service logs")
	require.FileExists(t, filepath.Join(output, "inspect.txt"),
		"a failing step must not stop the remaining captures")
}

func TestDiagnosticServicesExcludesProvisioningHelpers(t *testing.T) {
	inspection := strings.Join([]string{
		"111 clef-keystore-generation-el-clef-keystore RUNNING",
		"222 run-generate-genesis STOPPED",
		"333 validator-key-generation-cl-validator-keystore RUNNING",
		"444 el-1-gqrl-qrysm RUNNING",
		"555 cl-1-qrysm-gqrl RUNNING",
		"666 signer-clef RUNNING",
		"777 vc-1-gqrl-qrysm RUNNING",
	}, "\n")

	require.Equal(t, []string{
		"el-1-gqrl-qrysm",
		"cl-1-qrysm-gqrl",
		"signer-clef",
		"vc-1-gqrl-qrysm",
	}, diagnosticServices(inspection))
}
