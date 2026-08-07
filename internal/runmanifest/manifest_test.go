package runmanifest

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/stretchr/testify/require"
)

func TestCollect(t *testing.T) {
	environment := map[string]string{
		SourceGoQRLEnv:       "1111111111111111111111111111111111111111",
		SourceQrysmEnv:       "2222222222222222222222222222222222222222",
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
	manifest := Collect(t.Context(), Options{
		Backend:        devnet.BackendDocker,
		Images:         &images,
		PackageLocator: devnet.DefaultPackageLocator,
		Enclave:        "qrl-tests",
		TestsDir:       "/checkout",
		Lanes: []Lane{
			{Name: "execution-abi", Profile: devnet.ProfileSingle, Suites: []string{"execution-abi"}, Seed: 42},
		},
		Environ: func(key string) string { return environment[key] },
		Command: command,
		Now:     func() time.Time { return started },
	})

	require.Equal(t, "1111111111111111111111111111111111111111", manifest.Sources.GoQRL)
	require.Equal(t, "2222222222222222222222222222222222222222", manifest.Sources.Qrysm)
	require.Equal(t, "3333333333333333333333333333333333333333", manifest.Sources.QRLTests)
	require.Contains(t, probes, []string{"git", "-C", "/checkout", "rev-parse", "HEAD"})
	require.Equal(t, devnet.DefaultImages(), *manifest.Images)
	require.Equal(t, devnet.DefaultPackageLocator, manifest.PackageLocator)
	require.Equal(t, runtime.Version(), manifest.Versions.Go)
	require.Equal(t, "28.0.1", manifest.Versions.Docker)
	require.Equal(t, "1.20.1", manifest.Versions.Kurtosis)
	require.Equal(t, "cyyber/qrl-tests", manifest.GitHub.Repository)
	require.Equal(t, "12345", manifest.GitHub.RunID)
	require.Equal(t, "2", manifest.GitHub.RunAttempt)
	require.Equal(t, started, manifest.StartedAt)
	require.Empty(t, manifest.Result, "a starting manifest must not claim a result")
}

func TestCollectPrefersExplicitTestsRevision(t *testing.T) {
	manifest := Collect(t.Context(), Options{
		Environ: func(key string) string {
			if key == SourceQRLTestsEnv {
				return "4444444444444444444444444444444444444444"
			}
			return ""
		},
		Command: func(context.Context, string, ...string) (string, error) {
			return "", errors.New("probes must not run for configured revisions")
		},
	})
	require.Equal(t, "4444444444444444444444444444444444444444", manifest.Sources.QRLTests)
}

func TestCollectSurvivesMissingTools(t *testing.T) {
	manifest := Collect(t.Context(), Options{
		Environ: func(string) string { return "" },
		Command: func(context.Context, string, ...string) (string, error) {
			return "", errors.New("not installed")
		},
	})
	require.Empty(t, manifest.Versions.Docker)
	require.Empty(t, manifest.Versions.Kurtosis)
	require.Equal(t, runtime.Version(), manifest.Versions.Go)
}

func TestFinish(t *testing.T) {
	manifest := Manifest{Lanes: []Lane{{Name: "execution-abi"}, {Name: "consensus"}}}
	finished := time.Date(2026, 8, 7, 13, 0, 0, 0, time.UTC)

	manifest.Finish(map[string]string{"execution-abi": "passed", "consensus": "passed"}, finished)
	require.Equal(t, "passed", manifest.Result)
	require.Equal(t, finished, manifest.FinishedAt)

	manifest.Finish(map[string]string{"execution-abi": "passed", "consensus": "failed"}, finished)
	require.Equal(t, "failed", manifest.Result)

	manifest.Finish(map[string]string{"execution-abi": "passed"}, finished)
	require.Equal(t, "failed", manifest.Result, "a lane without a result never ran")
	require.Empty(t, manifest.Lanes[1].Result)
}

func TestWriteAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reports", FileName)
	written := Manifest{
		PackageLocator: devnet.DefaultPackageLocator,
		Backend:        devnet.BackendDocker,
		Lanes:          []Lane{{Name: "execution-abi", Profile: devnet.ProfileSingle, Seed: 42}},
		StartedAt:      time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC),
	}
	require.NoError(t, written.Write(path))

	read, err := Read(path)
	require.NoError(t, err)
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
	require.NotContains(t, body, "images")
	require.NotContains(t, body, "custom_parameters")
}
