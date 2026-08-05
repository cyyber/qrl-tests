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

type recordingNetworks struct {
	call  string
	start devnet.StartOptions
}

func (networks *recordingNetworks) Start(_ context.Context, options devnet.StartOptions) (devnet.Environment, error) {
	networks.call = "start"
	networks.start = options
	return devnet.Environment{}, nil
}

func (networks *recordingNetworks) Stop(_ context.Context, name string) error {
	networks.call = "stop:" + name
	return nil
}

func TestRun(t *testing.T) {
	const enclaveName = "go-qrl-devnet-test"

	paramsFile := filepath.Join(t.TempDir(), "params.json")
	require.NoError(t, os.WriteFile(paramsFile, []byte(`{"custom":true}`), 0o600))

	for _, test := range []struct {
		name, output, call string
		arguments          []string
		parameters         []byte
	}{
		{
			"start with custom parameters", "network ready\n", "start",
			[]string{
				"network", "start", "--enclave-name", enclaveName,
				"--execution-image", "local/go-qrl:test",
				"--params-file", paramsFile,
			},
			[]byte(`{"custom":true}`),
		},
		{"stop", "network stopped\n", "stop:" + enclaveName, []string{"network", "stop", "--enclave-name", enclaveName}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			networks := new(recordingNetworks)
			var stdout, stderr bytes.Buffer

			app := newApp(networks)
			app.Writer, app.ErrWriter = &stdout, &stderr

			require.NoError(t, app.RunContext(t.Context(), append([]string{"qrltest"}, test.arguments...)))
			require.Equal(t, test.output, stdout.String())
			require.Equal(t, test.call, networks.call)
			require.Empty(t, stderr.String())

			if test.call == "start" {
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
					Parameters: test.parameters,
					Profile:    devnet.ProfileSingle,
				}, networks.start)
			}
		})
	}
}
