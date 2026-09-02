package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/stretchr/testify/require"
)

func TestManifestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	want := Manifest{
		Lane:           "execution",
		Profile:        devnet.ProfileSingle,
		ExecutionImage: "registry.example/go-qrl@sha256:digest",
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
	path := filepath.Join(t.TempDir(), FileName)
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
