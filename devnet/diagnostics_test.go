package devnet

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyyber/qrl-tests/internal/testutil"
	"github.com/stretchr/testify/require"
)

const testInspection = `Name: qrl-tests-abi
========================================= Files Artifacts =========================================
UUID Name
1111 clef-key-seed
========================================== User Services ==========================================
UUID Name Status
aaaa run-generate-genesis STOPPED
bbbb clef-keystore-generation-el-clef-keystore RUNNING
cccc validator-key-generation-cl-validator-keystore RUNNING
dddd el-1-gqrl-qrysm RUNNING
eeee cl-1-qrysm-gqrl RUNNING
ffff signer-clef RUNNING
0123 vc-1-gqrl-qrysm RUNNING
`

func TestCollectDiagnostics(t *testing.T) {
	output := t.TempDir()
	var commands []string
	run := func(_ context.Context, output io.Writer, name string, arguments ...string) error {
		command := name + " " + strings.Join(arguments, " ")
		commands = append(commands, command)
		if command == "kurtosis enclave inspect qrl-tests-abi" {
			_, err := io.WriteString(output, testInspection)
			return err
		}
		if strings.Contains(command, "run-generate-genesis") {
			_, err := io.WriteString(output, "starting genesis\nel_premine_addrs: {\"seed\":\"0x010000abcd\"}\ngenesis failed\n")
			return err
		}
		_, err := io.WriteString(output, "captured "+arguments[len(arguments)-1])
		return err
	}

	require.NoError(t, collectDiagnostics(t.Context(), run, "qrl-tests-abi", output))

	require.Equal(t, []string{
		"kurtosis enclave inspect qrl-tests-abi",
		"kurtosis service logs --all qrl-tests-abi run-generate-genesis",
		"kurtosis service logs --all qrl-tests-abi clef-keystore-generation-el-clef-keystore",
		"kurtosis service logs --all qrl-tests-abi validator-key-generation-cl-validator-keystore",
		"kurtosis service logs --all qrl-tests-abi el-1-gqrl-qrysm",
		"kurtosis service logs --all qrl-tests-abi cl-1-qrysm-gqrl",
		"kurtosis service logs --all qrl-tests-abi signer-clef",
		"kurtosis service logs --all qrl-tests-abi vc-1-gqrl-qrysm",
	}, commands)

	genesisLog, err := os.ReadFile(filepath.Join(output, "services", "run-generate-genesis.log"))
	require.NoError(t, err)
	require.Equal(t, "starting genesis\nel_premine_addrs: {\"seed\":\"0x010000abcd\"}\ngenesis failed\n", string(genesisLog))

	executionLog, err := os.ReadFile(filepath.Join(output, "services", "el-1-gqrl-qrysm.log"))
	require.NoError(t, err)
	require.Equal(t, "captured el-1-gqrl-qrysm", string(executionLog))

	manifest := testutil.ReadJSON[diagnosticsManifest](t, filepath.Join(output, "diagnostics.json"))
	require.True(t, manifest.Inspection.Captured)
	require.Len(t, manifest.Services, 7)
}

func TestCollectDiagnosticsKeepsGoingOnFailures(t *testing.T) {
	output := t.TempDir()
	failedLog := filepath.Join(output, "services", "run-generate-genesis.log")
	require.NoError(t, os.MkdirAll(failedLog, 0o755))
	run := func(_ context.Context, output io.Writer, name string, arguments ...string) error {
		if name == "kurtosis" && arguments[0] == "enclave" {
			_, err := io.WriteString(output, testInspection)
			require.NoError(t, err)
			return errors.New("inspect unavailable")
		}
		if arguments[len(arguments)-1] == "clef-keystore-generation-el-clef-keystore" {
			_, _ = io.WriteString(output, "partial output")
			return errors.New("logs unavailable")
		}
		_, err := io.WriteString(output, "captured")
		return err
	}

	err := collectDiagnostics(t.Context(), run, "qrl-tests-abi", output)
	require.ErrorContains(t, err, "kurtosis enclave inspect qrl-tests-abi: inspect unavailable")
	require.ErrorContains(t, err, "write diagnostic "+failedLog)
	require.ErrorContains(t, err, "kurtosis service logs qrl-tests-abi clef-keystore-generation-el-clef-keystore")
	partialLog, readErr := os.ReadFile(filepath.Join(output, "services", "clef-keystore-generation-el-clef-keystore.log"))
	require.NoError(t, readErr)
	require.Equal(t, "partial output", string(partialLog))
	require.FileExists(t, filepath.Join(output, "services", "el-1-gqrl-qrysm.log"),
		"a failing service capture must not stop the remaining captures")

	manifest := testutil.ReadJSON[diagnosticsManifest](t, filepath.Join(output, "diagnostics.json"))
	require.Contains(t, manifest.Inspection.Error, "kurtosis enclave inspect qrl-tests-abi: inspect unavailable")
	require.False(t, manifest.Services[0].Captured)
	require.Contains(t, manifest.Services[0].Error, "write diagnostic")
	require.False(t, manifest.Services[1].Captured)
	require.Contains(t, manifest.Services[1].Error,
		"kurtosis service logs qrl-tests-abi clef-keystore-generation-el-clef-keystore: logs unavailable")
	require.True(t, manifest.Services[3].Captured)
}

func TestServiceNamesFromInspectionReadsOnlyUserServices(t *testing.T) {
	require.Equal(t, []string{
		"run-generate-genesis",
		"clef-keystore-generation-el-clef-keystore",
		"validator-key-generation-cl-validator-keystore",
		"el-1-gqrl-qrysm",
		"cl-1-qrysm-gqrl",
		"signer-clef",
		"vc-1-gqrl-qrysm",
	}, serviceNamesFromInspection(testInspection))
}
