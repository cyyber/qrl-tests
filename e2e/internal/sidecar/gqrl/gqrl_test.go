package gqrl

import (
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/stretchr/testify/require"
)

func TestConsoleSpec(t *testing.T) {
	for _, test := range []struct {
		name        string
		endpointURL string
		interactive bool
		wantCmd     []string
	}{
		{
			name:        "exec",
			endpointURL: "http://127.0.0.1:8545",
			wantCmd: []string{
				"attach", "--datadir", "/tmp/qrl-tests-console", "--jspath", "/tmp/testdata/console",
				"--exec", `loadScript("harness.js");loadScript("api.js")`,
				"http://host.docker.internal:8545",
			},
		},
		{
			name:        "interactive",
			endpointURL: "ws://127.0.0.1:8546",
			interactive: true,
			wantCmd: []string{
				"attach", "--datadir", "/tmp/qrl-tests-console", "--jspath", "/tmp/testdata/console",
				"--preload", "harness.js,api.js",
				"ws://host.docker.internal:8546",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec, err := consoleSpec(Config{
				Image:       "registry.example/go-qrl@sha256:digest",
				EndpointURL: test.endpointURL,
				Scripts: []sidecar.File{
					{Name: "harness.js", Body: []byte("// harness"), Mode: 0o444},
					{Name: "api.js", Body: []byte("// api")},
				},
				Run:         []string{"harness.js", "api.js"},
				Interactive: test.interactive,
			})
			require.NoError(t, err)

			require.Equal(t, "registry.example/go-qrl@sha256:digest", spec.Image)
			require.Equal(t, []string{"gqrl"}, spec.Entrypoint)
			require.Equal(t, test.wantCmd, spec.Cmd)
			require.Equal(t, test.interactive, spec.Stdin)
			require.Equal(t, []sidecar.File{
				{Name: "/tmp/testdata/console/harness.js", Body: []byte("// harness"), Mode: 0o444},
				{Name: "/tmp/testdata/console/api.js", Body: []byte("// api")},
			}, spec.Files)
		})
	}
}

func TestConsoleSpecRejectsInvalidConfig(t *testing.T) {
	for _, test := range []struct {
		name    string
		config  Config
		wantErr string
	}{
		{
			name:    "nothing to run",
			config:  Config{EndpointURL: "http://127.0.0.1:8545"},
			wantErr: "console has no scripts to run",
		},
		{
			name:    "bad endpoint",
			config:  Config{EndpointURL: "https://rpc.example", Run: []string{"a.js"}},
			wantErr: "rewrite console endpoint",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := consoleSpec(test.config)
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}
