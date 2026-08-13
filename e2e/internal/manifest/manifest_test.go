package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/stretchr/testify/require"
)

func TestManifestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	want := Manifest{
		Lane:    "execution",
		Profile: devnet.ProfileSingle,
		Tools:   Tools{GQRL: "/tmp/gqrl"},
		Environment: devnet.Environment{
			EnclaveName: "qrl-tests-execution",
			Backend:     devnet.BackendDocker,
			Participants: []devnet.Participant{{
				Index:     1,
				Execution: devnet.ExecutionService{RPCURL: "http://127.0.0.1:8545"},
				Consensus: devnet.ConsensusService{URL: "http://127.0.0.1:3500"},
			}},
		},
	}

	require.NoError(t, Write(path, want))
	got, err := Read(path)
	require.NoError(t, err)
	require.Equal(t, want, got)

	t.Setenv(PathEnv, path)
	configured, err := FromEnv()
	require.NoError(t, err)
	require.Equal(t, want, configured)
}

func TestManifestRequiresParticipant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	require.Error(t, Write(path, Manifest{}))

	require.NoError(t, os.WriteFile(path, []byte(`{"environment":{}}`), 0o600))
	_, err := Read(path)
	require.ErrorContains(t, err, path)
}

func TestFromEnvReportsMissingConfiguration(t *testing.T) {
	t.Setenv(PathEnv, "")
	_, err := FromEnv()
	require.ErrorContains(t, err, PathEnv)
}

func TestManifestOmitsEmptyTools(t *testing.T) {
	payload, err := json.Marshal(Manifest{})
	require.NoError(t, err)
	require.NotContains(t, string(payload), `"tools"`)
}
