package soak

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/internal/runmanifest"
	"github.com/cyyber/qrl-tests/internal/testutil"
	"github.com/stretchr/testify/require"
)

type recordingNetworks struct {
	mutex     sync.Mutex
	started   devnet.StartOptions
	inspected string
	stopped   []string
	collected []string
	stopErr   error
}

func (networks *recordingNetworks) Start(_ context.Context, options devnet.StartOptions) (devnet.Environment, error) {
	networks.mutex.Lock()
	defer networks.mutex.Unlock()
	networks.started = options
	return testEnvironment(options.EnclaveName, options.Backend), nil
}

func (networks *recordingNetworks) Inspect(_ context.Context, name string) (devnet.Environment, error) {
	networks.mutex.Lock()
	defer networks.mutex.Unlock()
	networks.inspected = name
	return testEnvironment(name, ""), nil
}

func (networks *recordingNetworks) Stop(_ context.Context, name string) error {
	networks.mutex.Lock()
	defer networks.mutex.Unlock()
	networks.stopped = append(networks.stopped, name)
	return networks.stopErr
}

func (networks *recordingNetworks) CollectDiagnostics(_ context.Context, _, outputDir string) error {
	networks.mutex.Lock()
	defer networks.mutex.Unlock()
	networks.collected = append(networks.collected, outputDir)
	return nil
}

func testEnvironment(name string, backend devnet.Backend) devnet.Environment {
	return devnet.Environment{
		EnclaveName: name,
		Backend:     backend,
		Participants: []devnet.Participant{{
			Index:     1,
			Execution: devnet.ExecutionService{RPCURL: "http://127.0.0.1:8545"},
			Consensus: devnet.ConsensusService{URL: "http://127.0.0.1:3500"},
		}},
	}
}

func passingEvaluation() Evaluation {
	return Evaluation{
		Passed:        true,
		Enforced:      false,
		Samples:       3,
		SteadySamples: 2,
		Gates: []Gate{{
			Name: "chain-progress/head", Passed: true,
			Observed: "12.00 blocks/min", Threshold: "≥ 10.00 blocks/min",
		}},
	}
}

func newTestRunner(t *testing.T, configuration Config, networks *recordingNetworks, evaluation Evaluation, sampleErr error) *Runner {
	t.Helper()
	runner := New(configuration, io.Discard, io.Discard)
	runner.networks = networks
	runner.sample = func(context.Context, devnet.Environment, string) (Evaluation, error) {
		return evaluation, sampleErr
	}
	return runner
}

func TestRunProvisionsWritesReportsAndStops(t *testing.T) {
	reports := t.TempDir()
	networks := new(recordingNetworks)
	runner := newTestRunner(t, Config{
		EnclaveName:  "qrl-soak",
		ReportDir:    reports,
		Backend:      devnet.BackendKubernetes,
		EndpointMode: devnet.EndpointModeCluster,
		LoadPercent:  30,
		Duration:     time.Minute,
		StartTimeout: time.Minute,
	}, networks, passingEvaluation(), nil)

	require.NoError(t, runner.Run(t.Context()))
	require.Equal(t, "qrl-soak", networks.started.EnclaveName)
	require.Equal(t, devnet.ProfileSoak, networks.started.Profile)
	require.Equal(t, devnet.BackendKubernetes, networks.started.Backend)
	require.Equal(t, devnet.EndpointModeCluster, networks.started.EndpointMode)
	require.Equal(t, 30, networks.started.LoadPercent)
	require.Equal(t, []string{"qrl-soak"}, networks.stopped)
	require.Empty(t, networks.collected)

	record := testutil.ReadJSON[Manifest](t, filepath.Join(reports, ManifestFile))
	require.Equal(t, devnet.ProfileSoak, record.Profile)
	require.Equal(t, 30, record.LoadPercent)

	verdict := testutil.ReadJSON[Verdict](t, filepath.Join(reports, VerdictFile))
	require.True(t, verdict.Passed)
	require.Equal(t, "passed", verdict.Class)
	require.FileExists(t, filepath.Join(reports, ResultsFile))
	require.FileExists(t, filepath.Join(reports, SummaryFile))
	require.FileExists(t, filepath.Join(reports, OutputLog))
	require.FileExists(t, filepath.Join(reports, runmanifest.FileName))
	results := testutil.ReadJSON[Evaluation](t, filepath.Join(reports, ResultsFile))
	require.Equal(t, devnet.PackageLocator, results.PackageLocator)
	require.NotEmpty(t, results.Images["execution"])
}

func TestRunExistingDoesNotStop(t *testing.T) {
	reports := t.TempDir()
	networks := new(recordingNetworks)
	runner := newTestRunner(t, Config{
		EnclaveName: "qrl-soak",
		ReportDir:   reports,
		Existing:    true,
		Duration:    time.Minute,
	}, networks, passingEvaluation(), nil)

	require.NoError(t, runner.Run(t.Context()))
	require.Equal(t, "qrl-soak", networks.inspected)
	require.Empty(t, networks.started.EnclaveName)
	require.Empty(t, networks.stopped)
}

func TestRunKeepNetwork(t *testing.T) {
	reports := t.TempDir()
	networks := new(recordingNetworks)
	runner := newTestRunner(t, Config{
		EnclaveName: "qrl-soak",
		ReportDir:   reports,
		KeepNetwork: true,
		Duration:    time.Minute,
	}, networks, passingEvaluation(), nil)

	require.NoError(t, runner.Run(t.Context()))
	require.Equal(t, "qrl-soak", networks.started.EnclaveName)
	require.Empty(t, networks.stopped)
}

func TestRunFailedGatesCollectsDiagnostics(t *testing.T) {
	reports := t.TempDir()
	networks := new(recordingNetworks)
	evaluation := Evaluation{
		Passed:   false,
		Enforced: true,
		Gates: []Gate{{
			Name: "chain-progress/head", Passed: false,
			Observed: "0.00 blocks/min", Threshold: "≥ 10.00 blocks/min",
		}},
	}
	runner := newTestRunner(t, Config{
		EnclaveName: "qrl-soak",
		ReportDir:   reports,
		Duration:    time.Minute,
		Enforce:     true,
	}, networks, evaluation, nil)

	err := runner.Run(t.Context())
	require.ErrorContains(t, err, "soak gates failed")
	require.Equal(t, []string{filepath.Join(reports, diagnosticsDirectory)}, networks.collected)
	require.Equal(t, []string{"qrl-soak"}, networks.stopped)

	verdict := testutil.ReadJSON[Verdict](t, filepath.Join(reports, VerdictFile))
	require.False(t, verdict.Passed)
	require.Equal(t, "product", verdict.Class)
}

func TestRunCleanupFailureIsReturned(t *testing.T) {
	reports := t.TempDir()
	networks := &recordingNetworks{stopErr: context.DeadlineExceeded}
	runner := newTestRunner(t, Config{
		EnclaveName: "qrl-soak",
		ReportDir:   reports,
		Duration:    time.Minute,
	}, networks, passingEvaluation(), nil)

	err := runner.Run(t.Context())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, []string{filepath.Join(reports, diagnosticsDirectory)}, networks.collected)
}

func TestRunRejectsExistingWithParameters(t *testing.T) {
	runner := New(Config{Existing: true, Parameters: []byte(`{}`), Duration: time.Minute}, io.Discard, io.Discard)
	require.ErrorContains(t, runner.Run(t.Context()), "custom parameters")
}

func TestVerdictClass(t *testing.T) {
	require.Equal(t, "passed", VerdictClass(passingEvaluation()))
	require.Equal(t, "infrastructure", VerdictClass(Evaluation{
		Passed: true,
		Gates: []Gate{
			{Name: "placement/pinned", Passed: false},
			{Name: "chain-progress/head", Passed: true},
		},
	}))
	require.Equal(t, "product", VerdictClass(Evaluation{
		Gates: []Gate{{Name: "chain-progress/head", Passed: false}},
	}))
}

func TestRenderSummary(t *testing.T) {
	markdown := RenderSummary(Evaluation{
		Passed:        false,
		Enforced:      true,
		Samples:       10,
		SteadySamples: 8,
		WarmupWindow:  time.Minute,
		SteadyWindow:  time.Hour,
		Gates: []Gate{{
			Name: "chain-progress/head", Passed: false,
			Observed: "1.00 blocks/min", Threshold: "≥ 10.00 blocks/min",
		}},
	})
	require.Contains(t, markdown, "# Soak failed (product)")
	require.Contains(t, markdown, "samples: 10 (8 steady)")
	require.Contains(t, markdown, "chain-progress/head")
	require.Contains(t, markdown, "| no |")
}

func TestNewResolvesDefaults(t *testing.T) {
	runner := New(Config{}, io.Discard, io.Discard)
	require.Equal(t, devnet.DefaultEnclaveName, runner.configuration.EnclaveName)
	require.Equal(t, DefaultReportDir, runner.configuration.ReportDir)
	require.Equal(t, DefaultDuration, runner.configuration.Duration)
	require.Equal(t, DefaultInterval, runner.configuration.Interval)
	require.Equal(t, DefaultStartTimeout, runner.configuration.StartTimeout)
}

func TestRunWritesSampleError(t *testing.T) {
	reports := t.TempDir()
	networks := new(recordingNetworks)
	runner := newTestRunner(t, Config{
		EnclaveName: "qrl-soak",
		ReportDir:   reports,
		Duration:    time.Minute,
	}, networks, Evaluation{}, errors.New("dial rpc"))

	require.ErrorContains(t, runner.Run(t.Context()), "dial rpc")
	require.Equal(t, []string{filepath.Join(reports, diagnosticsDirectory)}, networks.collected)
}

func TestRunPrintsCalibrationNote(t *testing.T) {
	var output bytes.Buffer
	reports := t.TempDir()
	networks := new(recordingNetworks)
	runner := New(Config{
		EnclaveName: "qrl-soak",
		ReportDir:   reports,
		Duration:    time.Minute,
	}, &output, io.Discard)
	runner.networks = networks
	runner.sample = func(context.Context, devnet.Environment, string) (Evaluation, error) {
		return passingEvaluation(), nil
	}

	require.NoError(t, runner.Run(t.Context()))
	require.Contains(t, output.String(), "SOAK_ENFORCE is off")
}
