// Package runmanifest records provenance and replay metadata for an E2E run:
// source revisions, image references, the qrl-package locator, lane seeds,
// tool versions, and the CI coordinates, written to reports/run-manifest.json.
package runmanifest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
)

const (
	FileName = "run-manifest.json"

	// The CI workflow records the client revisions it built or resolved;
	// local runs leave them unset.
	sourceGoQRLEnv     = "E2E_SOURCE_GO_QRL"
	sourceQrysmEnv     = "E2E_SOURCE_QRYSM"
	sourceGeneratorEnv = "E2E_SOURCE_GENERATOR"

	probeTimeout = 10 * time.Second
)

type Sources struct {
	GoQRL     string `json:"go_qrl,omitempty"`
	Qrysm     string `json:"qrysm,omitempty"`
	Generator string `json:"genesis_generator,omitempty"`
	QRLTests  string `json:"qrl_tests,omitempty"`
}

type Versions struct {
	Go       string `json:"go"`
	Docker   string `json:"docker,omitempty"`
	Kurtosis string `json:"kurtosis,omitempty"`
}

type GitHub struct {
	Repository string `json:"repository,omitempty"`
	Workflow   string `json:"workflow,omitempty"`
	RunID      string `json:"run_id,omitempty"`
	RunAttempt string `json:"run_attempt,omitempty"`
}

type Lane struct {
	Name    string         `json:"name"`
	Enclave string         `json:"enclave"`
	Profile devnet.Profile `json:"profile"`
	Suites  []string       `json:"suites"`
	Seed    int64          `json:"seed"`
	Result  string         `json:"result,omitempty"`
}

type Manifest struct {
	Sources Sources        `json:"sources"`
	Images  *devnet.Images `json:"images,omitempty"`
	// ParametersSHA256 fingerprints the custom parameters payload, so a
	// reproduction can verify it is feeding the network the same bytes.
	ParametersSHA256 string         `json:"custom_parameters_sha256,omitempty"`
	PackageLocator   string         `json:"qrl_package"`
	Backend          devnet.Backend `json:"backend"`
	Lanes            []Lane         `json:"lanes"`
	Versions         Versions       `json:"versions"`
	GitHub           GitHub         `json:"github,omitzero"`
	StartedAt        time.Time      `json:"started_at"`
	FinishedAt       time.Time      `json:"finished_at,omitzero"`
	Result           string         `json:"result,omitempty"`
}

type commandFunc func(context.Context, string, ...string) (string, error)

type dependencies struct {
	getenv func(string) string
	probe  commandFunc
	now    func() time.Time
}

// Collect enriches a starting manifest with source, tool, and CI metadata.
// Probes are best-effort: a missing tool leaves its field empty rather than
// failing the run the manifest is meant to explain.
func Collect(ctx context.Context, testsDir string, manifest Manifest) Manifest {
	return collect(ctx, testsDir, manifest, dependencies{
		getenv: os.Getenv,
		probe:  probeCommand,
		now:    time.Now,
	})
}

func collect(ctx context.Context, testsDir string, manifest Manifest, deps dependencies) Manifest {
	testsRevision, _ := deps.probe(ctx, "git", "-C", testsDir, "rev-parse", "HEAD")

	manifest.Sources = Sources{
		GoQRL:     deps.getenv(sourceGoQRLEnv),
		Qrysm:     deps.getenv(sourceQrysmEnv),
		Generator: deps.getenv(sourceGeneratorEnv),
		QRLTests:  testsRevision,
	}
	manifest.Versions = Versions{
		Go:       runtime.Version(),
		Docker:   dockerVersion(ctx, deps.probe),
		Kurtosis: kurtosisVersion(ctx, deps.probe),
	}
	manifest.GitHub = GitHub{
		Repository: deps.getenv("GITHUB_REPOSITORY"),
		Workflow:   deps.getenv("GITHUB_WORKFLOW"),
		RunID:      deps.getenv("GITHUB_RUN_ID"),
		RunAttempt: deps.getenv("GITHUB_RUN_ATTEMPT"),
	}
	manifest.StartedAt = deps.now().UTC()
	return manifest
}

// Finish records the per-lane and overall outcomes; lanes without an entry in
// results keep an empty result, marking runs that never reached them.
func (manifest *Manifest) Finish(results map[string]bool, finishedAt time.Time) {
	overall := "passed"
	for index := range manifest.Lanes {
		lane := &manifest.Lanes[index]
		passed, found := results[lane.Name]
		if !found {
			lane.Result = ""
			overall = "failed"
			continue
		}
		if passed {
			lane.Result = "passed"
		} else {
			lane.Result = "failed"
			overall = "failed"
		}
	}
	manifest.FinishedAt = finishedAt.UTC()
	manifest.Result = overall
}

func (manifest Manifest) Write(path string) error {
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode run manifest: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create run manifest directory: %w", err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		return fmt.Errorf("write run manifest: %w", err)
	}
	return nil
}

func dockerVersion(ctx context.Context, command commandFunc) string {
	version, _ := command(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	return version
}

func kurtosisVersion(ctx context.Context, command commandFunc) string {
	output, _ := command(ctx, "kurtosis", "version")
	for line := range strings.Lines(output) {
		if version, found := strings.CutPrefix(line, "CLI Version:"); found {
			return strings.TrimSpace(version)
		}
	}
	return ""
}

func probeCommand(ctx context.Context, name string, arguments ...string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	output, err := exec.CommandContext(probeCtx, name, arguments...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
