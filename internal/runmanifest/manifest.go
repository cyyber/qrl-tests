// Package runmanifest records everything needed to reproduce an E2E run:
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
	SourceGoQRLEnv    = "E2E_SOURCE_GO_QRL"
	SourceQrysmEnv    = "E2E_SOURCE_QRYSM"
	SourceQRLTestsEnv = "E2E_SOURCE_QRL_TESTS"

	probeTimeout = 10 * time.Second
)

type Sources struct {
	GoQRL    string `json:"go_qrl,omitempty"`
	Qrysm    string `json:"qrysm,omitempty"`
	QRLTests string `json:"qrl_tests,omitempty"`
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
	Profile devnet.Profile `json:"profile"`
	Suites  []string       `json:"suites"`
	Seed    int64          `json:"seed"`
	Result  string         `json:"result,omitempty"`
}

type Manifest struct {
	Sources          Sources        `json:"sources"`
	Images           *devnet.Images `json:"images,omitempty"`
	CustomParameters bool           `json:"custom_parameters,omitempty"`
	// ParametersSHA256 fingerprints the custom parameters payload, so a
	// reproduction can verify it is feeding the network the same bytes.
	ParametersSHA256 string         `json:"custom_parameters_sha256,omitempty"`
	PackageLocator   string         `json:"qrl_package"`
	Backend          devnet.Backend `json:"backend"`
	Enclave          string         `json:"enclave"`
	Lanes            []Lane         `json:"lanes"`
	Versions         Versions       `json:"versions"`
	GitHub           GitHub         `json:"github"`
	StartedAt        time.Time      `json:"started_at"`
	FinishedAt       time.Time      `json:"finished_at,omitzero"`
	Result           string         `json:"result,omitempty"`
}

// Options carries the run configuration into Collect. The injection points
// default to the live environment and real commands when unset.
type Options struct {
	Backend          devnet.Backend
	Images           *devnet.Images
	CustomParameters bool
	ParametersSHA256 string
	PackageLocator   string
	Enclave          string
	TestsDir         string
	Lanes            []Lane

	Environ func(string) string
	Command func(ctx context.Context, name string, arguments ...string) (string, error)
	Now     func() time.Time
}

// Collect assembles the manifest for a starting run. Version and revision
// probes are best-effort: a missing tool leaves its field empty rather than
// failing the run the manifest is meant to explain.
func Collect(ctx context.Context, options Options) Manifest {
	environ := options.Environ
	if environ == nil {
		environ = os.Getenv
	}
	command := options.Command
	if command == nil {
		command = runCommand
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}

	testsRevision := environ(SourceQRLTestsEnv)
	if testsRevision == "" {
		testsRevision, _ = command(ctx, "git", "-C", options.TestsDir, "rev-parse", "HEAD")
	}

	return Manifest{
		Sources: Sources{
			GoQRL:    environ(SourceGoQRLEnv),
			Qrysm:    environ(SourceQrysmEnv),
			QRLTests: testsRevision,
		},
		Images:           options.Images,
		CustomParameters: options.CustomParameters,
		ParametersSHA256: options.ParametersSHA256,
		PackageLocator:   options.PackageLocator,
		Backend:          options.Backend,
		Enclave:          options.Enclave,
		Lanes:            options.Lanes,
		Versions: Versions{
			Go:       runtime.Version(),
			Docker:   dockerVersion(ctx, command),
			Kurtosis: kurtosisVersion(ctx, command),
		},
		GitHub: GitHub{
			Repository: environ("GITHUB_REPOSITORY"),
			Workflow:   environ("GITHUB_WORKFLOW"),
			RunID:      environ("GITHUB_RUN_ID"),
			RunAttempt: environ("GITHUB_RUN_ATTEMPT"),
		},
		StartedAt: now().UTC(),
	}
}

// Finish records the per-lane and overall outcomes; lanes without an entry in
// results keep an empty result, marking runs that never reached them.
func (manifest *Manifest) Finish(results map[string]string, finishedAt time.Time) {
	overall := "passed"
	for index := range manifest.Lanes {
		lane := &manifest.Lanes[index]
		lane.Result = results[lane.Name]
		if lane.Result != "passed" {
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

func Read(path string) (Manifest, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read run manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode run manifest: %w", err)
	}
	return manifest, nil
}

func dockerVersion(ctx context.Context, command func(context.Context, string, ...string) (string, error)) string {
	version, _ := command(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	return version
}

func kurtosisVersion(ctx context.Context, command func(context.Context, string, ...string) (string, error)) string {
	output, _ := command(ctx, "kurtosis", "version")
	for line := range strings.Lines(output) {
		if version, found := strings.CutPrefix(line, "CLI Version:"); found {
			return strings.TrimSpace(version)
		}
	}
	return ""
}

func runCommand(ctx context.Context, name string, arguments ...string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	output, err := exec.CommandContext(probeCtx, name, arguments...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
