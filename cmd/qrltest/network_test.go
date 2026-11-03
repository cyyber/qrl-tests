package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/stretchr/testify/require"
)

type recordingController struct {
	startOptions   devnet.StartOptions
	stoppedEnclave string
}

func (controller *recordingController) Start(_ context.Context, options devnet.StartOptions) (devnet.Environment, error) {
	controller.startOptions = options
	return devnet.Environment{}, nil
}

func (controller *recordingController) Stop(_ context.Context, name string) error {
	controller.stoppedEnclave = name
	return nil
}

func TestNetworkStart(t *testing.T) {
	const enclaveName = "go-qrl-devnet-test"

	paramsFile := filepath.Join(t.TempDir(), "params.yaml")
	parameters := []byte("custom: true\n")
	require.NoError(t, os.WriteFile(paramsFile, parameters, 0o600))

	controller := new(recordingController)
	output := runCommand(t, controller,
		"network", "start", "--enclave-name", enclaveName,
		"--execution-image", "local/go-qrl:test",
		"--params-file", paramsFile,
	)

	require.Equal(t, "network ready\n", output)
	require.Equal(t, devnet.StartOptions{
		EnclaveName: enclaveName,
		Backend:     devnet.BackendDocker,
		Images: devnet.Images{
			Execution: "local/go-qrl:test",
			Clef:      devnet.DefaultClefImage,
			Consensus: devnet.DefaultConsensusImage,
			Validator: devnet.DefaultValidatorImage,
			Genesis:   devnet.DefaultGenesisImage,

			RemoteSigner: devnet.DefaultRemoteSignerImage,
		},
		Parameters: parameters,
		Profile:    devnet.ProfileSingle,
	}, controller.startOptions)
}

func TestNetworkParams(t *testing.T) {
	output := runCommand(t, new(recordingController),
		"network", "params", "--profile", "remote-signer",
		"--validator-image", "local/qrysm:remote-signer-validator",
	)

	want, err := devnet.ProfileParameters(devnet.ProfileRemoteSigner, devnet.Images{
		Execution:    devnet.DefaultExecutionImage,
		Clef:         devnet.DefaultClefImage,
		Consensus:    devnet.DefaultConsensusImage,
		Validator:    "local/qrysm:remote-signer-validator",
		Genesis:      devnet.DefaultGenesisImage,
		RemoteSigner: devnet.DefaultRemoteSignerImage,
	})
	require.NoError(t, err)
	require.Equal(t, want+"\n", output)
	require.Contains(t, output, `"remote_signer_type":"go-remotesigner"`)
}

func TestNetworkStop(t *testing.T) {
	const enclaveName = "go-qrl-devnet-test"

	controller := new(recordingController)
	output := runCommand(t, controller, "network", "stop", "--enclave-name", enclaveName)

	require.Equal(t, "network stopped\n", output)
	require.Equal(t, enclaveName, controller.stoppedEnclave)
}

func runCommand(t *testing.T, network networkController, arguments ...string) string {
	t.Helper()

	var stdout, stderr bytes.Buffer
	app := newApp(network)
	app.Writer, app.ErrWriter = &stdout, &stderr

	require.NoError(t, app.RunContext(t.Context(), append([]string{"qrltest"}, arguments...)))
	require.Empty(t, stderr.String())
	return stdout.String()
}
