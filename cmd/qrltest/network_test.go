package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/stretchr/testify/require"
)

type recordingController struct {
	startOptions   devnet.StartOptions
	startDeadline  bool
	stoppedEnclave string
}

func (controller *recordingController) Start(ctx context.Context, options devnet.StartOptions) (devnet.Environment, error) {
	controller.startOptions = options
	_, controller.startDeadline = ctx.Deadline()
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
		"--timeout", "90s",
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
		},
		Parameters: parameters,
		Profile:    devnet.ProfileSingle,
		Timeout:    90 * time.Second,
	}, controller.startOptions)
	require.False(t, controller.startDeadline, "the manager applies the timeout")
}

func TestNetworkStartUnsetTimeout(t *testing.T) {
	t.Setenv("DEVNET_START_TIMEOUT", "")

	controller := new(recordingController)
	runCommand(t, controller, "network", "start", "--profile", "mainnet-slot-time")
	require.Equal(t, devnet.ProfileMainnetSlotTime, controller.startOptions.Profile)
	require.Zero(t, controller.startOptions.Timeout)
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
