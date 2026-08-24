package runmanifest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/stretchr/testify/require"
)

func TestEnrich(t *testing.T) {
	environment := map[string]string{
		sourceGoQRLEnv:       "1111111111111111111111111111111111111111",
		sourceQrysmEnv:       "2222222222222222222222222222222222222222",
		sourceGeneratorEnv:   "4444444444444444444444444444444444444444",
		"GITHUB_REPOSITORY":  "cyyber/qrl-tests",
		"GITHUB_WORKFLOW":    "nightly",
		"GITHUB_RUN_ID":      "12345",
		"GITHUB_RUN_ATTEMPT": "2",
	}
	var probes [][]string
	probe := func(_ context.Context, name string, arguments ...string) (string, error) {
		probes = append(probes, append([]string{name}, arguments...))
		switch name {
		case "git":
			return "3333333333333333333333333333333333333333", nil
		case "kurtosis":
			return "CLI Version:   1.20.1\n\nEngine Version: 1.20.1", nil
		}
		return "", errors.New("unexpected probe")
	}
	started := time.Date(2026, 8, 7, 12, 0, 0, 0, time.FixedZone("UTC+4", 4*60*60))

	images := devnet.DefaultImages()
	manifest := enrich(t.Context(), "/checkout", Manifest{
		Backend:        devnet.BackendDocker,
		Images:         &images,
		PackageLocator: devnet.PackageLocator,
		Lanes: []Lane{
			{Name: "execution", Enclave: "qrl-tests", Profile: devnet.ProfileSingle, Suites: []string{"execution-abi"}, Seed: 42},
		},
	}, dependencies{
		getenv: func(key string) string { return environment[key] },
		probe:  probe,
		dockerVersion: func(context.Context) string {
			return "28.0.1"
		},
		now: func() time.Time { return started },
	})

	require.Equal(t, Sources{
		GoQRL:     "1111111111111111111111111111111111111111",
		Qrysm:     "2222222222222222222222222222222222222222",
		Generator: "4444444444444444444444444444444444444444",
		QRLTests:  "3333333333333333333333333333333333333333",
	}, manifest.Sources)
	require.Equal(t, Versions{
		Go:       runtime.Version(),
		Docker:   "28.0.1",
		Kurtosis: "1.20.1",
	}, manifest.Versions)
	require.Equal(t, GitHub{
		Repository: "cyyber/qrl-tests",
		Workflow:   "nightly",
		RunID:      "12345",
		RunAttempt: "2",
	}, manifest.GitHub)
	require.Equal(t, [][]string{
		{"git", "-C", "/checkout", "rev-parse", "HEAD"},
		{"kurtosis", "version"},
	}, probes)
	require.NotNil(t, manifest.Images)
	require.Equal(t, devnet.DefaultImages(), *manifest.Images)
	require.Equal(t, devnet.PackageLocator, manifest.PackageLocator)
	require.Equal(t, started.UTC(), manifest.StartedAt)
	require.Empty(t, manifest.Result, "a starting manifest must not claim a result")
}

func TestEnrichSurvivesMissingTools(t *testing.T) {
	manifest := enrich(t.Context(), ".", Manifest{}, dependencies{
		getenv: func(string) string { return "" },
		probe: func(context.Context, string, ...string) (string, error) {
			return "", errors.New("not installed")
		},
		dockerVersion: func(context.Context) string {
			return ""
		},
		now: time.Now,
	})
	require.Empty(t, manifest.Versions.Docker)
	require.Empty(t, manifest.Versions.Kurtosis)
	require.Equal(t, runtime.Version(), manifest.Versions.Go)
}

func TestDockerVersion(t *testing.T) {
	var unavailable atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if unavailable.Load() {
			http.Error(writer, "unavailable", http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"Version":" 28.0.1\n","ApiVersion":"1.52","MinAPIVersion":"1.24"}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("DOCKER_CONFIG", t.TempDir())
	t.Setenv("DOCKER_HOST", "tcp://"+server.Listener.Addr().String())
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_TLS", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	t.Setenv("DOCKER_API_VERSION", "")
	t.Setenv("DOCKER_CUSTOM_HEADERS", "")

	require.Equal(t, "28.0.1", dockerVersion(t.Context()))

	unavailable.Store(true)
	require.Empty(t, dockerVersion(t.Context()))
}

func TestFinish(t *testing.T) {
	manifest := Manifest{Lanes: []Lane{{Name: "execution"}, {Name: "consensus"}}}
	finished := time.Date(2026, 8, 7, 13, 0, 0, 0, time.FixedZone("UTC+4", 4*60*60))

	manifest.Finish(map[string]bool{"execution": true, "consensus": true}, finished)
	require.Equal(t, "passed", manifest.Result)
	require.Equal(t, "passed", manifest.Lanes[0].Result)
	require.Equal(t, "passed", manifest.Lanes[1].Result)
	require.Equal(t, finished.UTC(), manifest.FinishedAt)

	manifest.Finish(map[string]bool{"execution": true, "consensus": false}, finished)
	require.Equal(t, "failed", manifest.Result)
	require.Equal(t, "failed", manifest.Lanes[1].Result)

	manifest.Finish(map[string]bool{"execution": true}, finished)
	require.Equal(t, "failed", manifest.Result, "a lane without a result never ran")
	require.Empty(t, manifest.Lanes[1].Result)
}

func TestWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reports", FileName)
	images := devnet.Images{
		Execution: "execution-image",
		Clef:      "clef-image",
		Consensus: "consensus-image",
		Validator: "validator-image",
		Genesis:   "genesis-image",
	}
	manifest := Manifest{
		Sources: Sources{
			GoQRL:     "go-qrl-revision",
			Qrysm:     "qrysm-revision",
			Generator: "generator-revision",
			QRLTests:  "qrl-tests-revision",
		},
		Images:           &images,
		ParametersSHA256: "parameters-sha256",
		PackageLocator:   "qrl-package-locator",
		Backend:          devnet.BackendDocker,
		Lanes: []Lane{{
			Name:    "execution",
			Enclave: "qrl-tests-execution",
			Profile: devnet.ProfileSingle,
			Suites:  []string{"execution-abi", "execution-console"},
			Seed:    42,
		}},
		Versions: Versions{Go: "go1.26.0", Docker: "28.0.1", Kurtosis: "1.20.1"},
		GitHub: GitHub{
			Repository: "cyyber/qrl-tests",
			Workflow:   "nightly",
			RunID:      "12345",
			RunAttempt: "2",
		},
		StartedAt: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC),
	}
	require.NoError(t, manifest.Write(path))

	payload, err := os.ReadFile(path)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"sources": {
			"go_qrl": "go-qrl-revision",
			"qrysm": "qrysm-revision",
			"genesis_generator": "generator-revision",
			"qrl_tests": "qrl-tests-revision"
		},
		"images": {
			"execution": "execution-image",
			"clef": "clef-image",
			"consensus": "consensus-image",
			"validator": "validator-image",
			"genesis": "genesis-image"
		},
		"custom_parameters_sha256": "parameters-sha256",
		"qrl_package": "qrl-package-locator",
		"backend": "docker",
		"lanes": [{
			"name": "execution",
			"enclave": "qrl-tests-execution",
			"profile": "single",
			"suites": ["execution-abi", "execution-console"],
			"seed": 42
		}],
		"versions": {
			"go": "go1.26.0",
			"docker": "28.0.1",
			"kurtosis": "1.20.1"
		},
		"github": {
			"repository": "cyyber/qrl-tests",
			"workflow": "nightly",
			"run_id": "12345",
			"run_attempt": "2"
		},
		"started_at": "2026-08-07T12:00:00Z"
	}`, string(payload))
	require.NotContains(t, string(payload), `"finished_at"`)
	require.NotContains(t, string(payload), `"result"`)
}

func TestKurtosisVersionParsing(t *testing.T) {
	probe := func(_ context.Context, name string, _ ...string) (string, error) {
		require.Equal(t, "kurtosis", name)
		return "no version header", nil
	}
	require.Empty(t, kurtosisVersion(t.Context(), probe))
}

func TestManifestOmitsUnsetFields(t *testing.T) {
	payload, err := json.Marshal(Manifest{})
	require.NoError(t, err)
	body := string(payload)
	require.NotContains(t, body, "finished_at")
	require.NotContains(t, body, "github")
	require.NotContains(t, body, "images")
	require.NotContains(t, body, "custom_parameters")
}
