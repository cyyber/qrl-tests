package runmanifest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/stretchr/testify/require"
)

func TestEnrich(t *testing.T) {
	environment := map[string]string{
		sourceGoQRLEnv:       "1111111111111111111111111111111111111111",
		sourceQrysmEnv:       "2222222222222222222222222222222222222222",
		"GITHUB_REPOSITORY":  "cyyber/qrl-tests",
		"GITHUB_WORKFLOW":    "nightly",
		"GITHUB_RUN_ID":      "12345",
		"GITHUB_RUN_ATTEMPT": "2",
	}
	var probes [][]string
	command := func(_ context.Context, name string, arguments ...string) (string, error) {
		probes = append(probes, append([]string{name}, arguments...))
		switch name {
		case "git":
			return "3333333333333333333333333333333333333333", nil
		case "docker":
			return "28.0.1", nil
		case "kurtosis":
			return "CLI Version:   1.20.1\n\nEngine Version: 1.20.1", nil
		}
		return "", errors.New("unexpected probe")
	}
	started := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	images := devnet.DefaultImages()
	manifest := enrich(t.Context(), "/checkout", Manifest{
		Backend:        devnet.BackendDocker,
		Images:         &images,
		PackageLocator: devnet.PackageLocator,
		Lanes: []Lane{
			{Name: "execution-abi", Enclave: "qrl-tests", Profile: devnet.ProfileSingle, Suites: []string{"execution-abi"}, Seed: 42},
		},
	}, dependencies{
		getenv: func(key string) string { return environment[key] },
		probe:  command,
		now:    func() time.Time { return started },
	})

	require.Equal(t, "1111111111111111111111111111111111111111", manifest.Sources.GoQRL)
	require.Equal(t, "2222222222222222222222222222222222222222", manifest.Sources.Qrysm)
	require.Equal(t, "3333333333333333333333333333333333333333", manifest.Sources.QRLTests)
	require.Contains(t, probes, []string{"git", "-C", "/checkout", "rev-parse", "HEAD"})
	require.Equal(t, devnet.DefaultImages(), *manifest.Images)
	require.Equal(t, devnet.PackageLocator, manifest.PackageLocator)
	require.Equal(t, runtime.Version(), manifest.Versions.Go)
	require.Equal(t, "28.0.1", manifest.Versions.Docker)
	require.Equal(t, "1.20.1", manifest.Versions.Kurtosis)
	require.Equal(t, "cyyber/qrl-tests", manifest.GitHub.Repository)
	require.Equal(t, "12345", manifest.GitHub.RunID)
	require.Equal(t, "2", manifest.GitHub.RunAttempt)
	require.Equal(t, started, manifest.StartedAt)
	require.Empty(t, manifest.Result, "a starting manifest must not claim a result")
}

func TestEnrichSurvivesMissingTools(t *testing.T) {
	manifest := enrich(t.Context(), ".", Manifest{}, dependencies{
		getenv: func(string) string { return "" },
		probe: func(context.Context, string, ...string) (string, error) {
			return "", errors.New("not installed")
		},
		now: time.Now,
	})
	require.Empty(t, manifest.Versions.Docker)
	require.Empty(t, manifest.Versions.Kurtosis)
	require.Equal(t, runtime.Version(), manifest.Versions.Go)
}

func TestFinish(t *testing.T) {
	manifest := Manifest{Lanes: []Lane{{Name: "execution-abi"}, {Name: "consensus"}}}
	finished := time.Date(2026, 8, 7, 13, 0, 0, 0, time.UTC)

	manifest.Finish(map[string]bool{"execution-abi": true, "consensus": true}, finished)
	require.Equal(t, "passed", manifest.Result)
	require.Equal(t, finished, manifest.FinishedAt)

	manifest.Finish(map[string]bool{"execution-abi": true, "consensus": false}, finished)
	require.Equal(t, "failed", manifest.Result)
	require.Equal(t, "failed", manifest.Lanes[1].Result)

	manifest.Finish(map[string]bool{"execution-abi": true}, finished)
	require.Equal(t, "failed", manifest.Result, "a lane without a result never ran")
	require.Empty(t, manifest.Lanes[1].Result)
}

func TestWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reports", FileName)
	written := Manifest{
		PackageLocator: devnet.PackageLocator,
		Backend:        devnet.BackendDocker,
		Lanes:          []Lane{{Name: "execution-abi", Enclave: "qrl-tests", Profile: devnet.ProfileSingle, Seed: 42}},
		StartedAt:      time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC),
	}
	require.NoError(t, written.Write(path))

	payload, err := os.ReadFile(path)
	require.NoError(t, err)
	var read Manifest
	require.NoError(t, json.Unmarshal(payload, &read))
	require.Equal(t, written, read)
}

func TestKurtosisVersionParsing(t *testing.T) {
	command := func(_ context.Context, name string, _ ...string) (string, error) {
		require.Equal(t, "kurtosis", name)
		return "no version header", nil
	}
	require.Empty(t, kurtosisVersion(t.Context(), command))
}

func TestManifestOmitsUnsetSections(t *testing.T) {
	payload, err := json.Marshal(Manifest{})
	require.NoError(t, err)
	body := string(payload)
	require.NotContains(t, body, "finished_at")
	require.NotContains(t, body, "github")
	require.NotContains(t, body, "images")
	require.NotContains(t, body, "custom_parameters")
}
