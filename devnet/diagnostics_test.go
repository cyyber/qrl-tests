package devnet

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	run := func(_ context.Context, name string, arguments ...string) (string, error) {
		command := name + " " + strings.Join(arguments, " ")
		commands = append(commands, command)
		if command == "kurtosis enclave inspect qrl-tests-abi" {
			return testInspection, nil
		}
		if strings.Contains(command, "run-generate-genesis") {
			return "starting genesis\nel_premine_addrs: {\"seed\":\"0x010000abcd\"}\ngenesis failed\n", nil
		}
		return "captured " + arguments[len(arguments)-1], nil
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
	require.Equal(t, "starting genesis\n[redacted sensitive diagnostic output]\ngenesis failed\n", string(genesisLog))
	require.NotContains(t, string(genesisLog), "0x010000abcd")

	executionLog, err := os.ReadFile(filepath.Join(output, "services", "el-1-gqrl-qrysm.log"))
	require.NoError(t, err)
	require.Equal(t, "captured el-1-gqrl-qrysm", string(executionLog))

	report := readDiagnosticsManifest(t, filepath.Join(output, "diagnostics.json"))
	require.True(t, report.Inspection.Captured)
	require.Len(t, report.Services, 7)
	require.True(t, report.Services[0].Sanitized)
	require.False(t, report.Services[3].Sanitized)
}

func TestCollectDiagnosticsKeepsGoingOnFailures(t *testing.T) {
	output := t.TempDir()
	run := func(_ context.Context, name string, arguments ...string) (string, error) {
		if name == "kurtosis" && arguments[0] == "enclave" {
			return testInspection, nil
		}
		if arguments[len(arguments)-1] == "run-generate-genesis" {
			return "genesis output", errors.New("logs unavailable")
		}
		return "captured", nil
	}

	err := collectDiagnostics(t.Context(), run, "qrl-tests-abi", output)
	require.ErrorContains(t, err, "kurtosis service logs qrl-tests-abi run-generate-genesis")
	require.FileExists(t, filepath.Join(output, "services", "el-1-gqrl-qrysm.log"),
		"a failing service capture must not stop the remaining captures")

	report := readDiagnosticsManifest(t, filepath.Join(output, "diagnostics.json"))
	require.False(t, report.Services[0].Captured)
	require.Equal(t, "logs unavailable", report.Services[0].Error)
	require.True(t, report.Services[3].Captured)
}

func TestDiagnosticServicesReadsOnlyUserServices(t *testing.T) {
	require.Equal(t, []string{
		"run-generate-genesis",
		"clef-keystore-generation-el-clef-keystore",
		"validator-key-generation-cl-validator-keystore",
		"el-1-gqrl-qrysm",
		"cl-1-qrysm-gqrl",
		"signer-clef",
		"vc-1-gqrl-qrysm",
	}, diagnosticServices(testInspection))
}

func TestSanitizeProvisioningLog(t *testing.T) {
	input := strings.Join([]string{
		"starting",
		"seed=0x010000abcd",
		"password=hunter2",
		"keystore generated",
		"jwt=deadbeef",
		"failed to connect",
		"private_key=abcd",
		"done",
	}, "\n") + "\n"

	require.Equal(t, strings.Join([]string{
		"starting",
		"[redacted sensitive diagnostic output]",
		"keystore generated",
		"[redacted sensitive diagnostic output]",
		"failed to connect",
		"[redacted sensitive diagnostic output]",
		"done",
	}, "\n")+"\n", sanitizeProvisioningLog(input))
}

func readDiagnosticsManifest(t *testing.T, path string) diagnosticsManifest {
	t.Helper()
	payload, err := os.ReadFile(path)
	require.NoError(t, err)
	var report diagnosticsManifest
	require.NoError(t, json.Unmarshal(payload, &report))
	return report
}
