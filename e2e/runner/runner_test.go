package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/lanes"
	"github.com/cyyber/qrl-tests/e2e/internal/manifest"
	"github.com/cyyber/qrl-tests/internal/results"
	"github.com/cyyber/qrl-tests/internal/runmanifest"
	"github.com/cyyber/qrl-tests/internal/testutil"
	"github.com/onsi/ginkgo/v2/types"
	"github.com/stretchr/testify/require"
)

const (
	executionLaneName = "execution"
	executionABISuite = "execution-abi"
)

type recordingNetworks struct {
	mutex      sync.Mutex
	started    devnet.StartOptions
	inspected  string
	stopped    []string
	collected  []string
	startErr   error
	stopErr    error
	collectErr error
}

func newTestRunner(t *testing.T, configuration Config, stdout, stderr io.Writer) *Runner {
	t.Helper()
	runner := New(configuration, stdout, stderr)
	runner.prepareGQRL = func(_ context.Context, _ runMode, _ devnet.Backend, _, _, destination string) error {
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return os.WriteFile(destination, []byte("gqrl"), 0o755)
	}
	return runner
}

func (networks *recordingNetworks) Start(_ context.Context, options devnet.StartOptions) (devnet.Environment, error) {
	networks.mutex.Lock()
	defer networks.mutex.Unlock()
	networks.started = options
	if networks.startErr != nil {
		return devnet.Environment{}, networks.startErr
	}
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

func (networks *recordingNetworks) CollectDiagnostics(_ context.Context, enclave, outputDir string) error {
	networks.mutex.Lock()
	defer networks.mutex.Unlock()
	// Tests assert on the recorded value: collecting after the enclave is
	// gone is an ordering bug, not a different output directory.
	if slices.Contains(networks.stopped, enclave) {
		outputDir = "after-stop:" + outputDir
	}
	networks.collected = append(networks.collected, outputDir)
	return networks.collectErr
}

func TestRunBuildsCommandAndCleansUp(t *testing.T) {
	reports := t.TempDir()
	networks := new(recordingNetworks)
	var command commandSpec
	var output bytes.Buffer
	runner := newTestRunner(t, Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
		Parameters:   []byte(`{"custom":true}`),
		Suites:       []string{executionABISuite},
	}, &output, &output)
	runner.networks = networks
	runner.prepareGQRL = func(context.Context, runMode, devnet.Backend, string, string, string) error {
		t.Fatal("ABI-only selection must not extract gqrl")
		return nil
	}
	runner.runCommand = func(_ context.Context, specification commandSpec) error {
		writeGinkgoReport(t, filepath.Join(reports, "lanes", executionLaneName), types.SpecStatePassed)
		command = specification
		return nil
	}

	require.NoError(t, runner.Run(t.Context(), executionLaneName))
	require.Equal(t, "qrl-tests", networks.started.EnclaveName)
	require.Equal(t, devnet.ProfileSingle, networks.started.Profile)
	require.Equal(t, []byte(`{"custom":true}`), networks.started.Parameters)
	require.Equal(t, []string{"qrl-tests"}, networks.stopped)
	require.Equal(t, "go", command.Path)
	require.Contains(t, command.Args, "./e2e/suites/execution/abi")
	workingDirectory, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, workingDirectory, command.Dir)
	require.Contains(t, command.Env, "PATH="+os.Getenv("PATH"))

	manifestPath := filepath.Join(reports, "lanes", executionLaneName, manifest.FileName)
	written, err := manifest.Read(manifestPath)
	require.NoError(t, err)
	require.Equal(t, "execution", written.Lane)
	require.Equal(t, devnet.ProfileSingle, written.Profile)
	require.Equal(t, testEnvironment("qrl-tests", devnet.BackendDocker), written.Environment)
	require.Contains(t, command.Env, manifest.PathEnv+"="+manifestPath)
	require.FileExists(t, filepath.Join(reports, "lanes", executionLaneName, "output.log"))
	require.Contains(t, output.String(), "=== RUN lane=execution profile=single ===")

	recordPath := filepath.Join(reports, runmanifest.FileName)
	record := testutil.ReadJSON[runmanifest.Manifest](t, recordPath)
	require.Equal(t, "c0b29628173dba03445f2a6b7f07aa6b5958f93af975feefff9ee025d4cc0c10", record.ParametersSHA256)
	payload, err := os.ReadFile(recordPath)
	require.NoError(t, err)
	require.NotContains(t, string(payload), `"custom_parameters":`)
}

func TestRunExtractsConsoleToolFromExecutionImage(t *testing.T) {
	reports := t.TempDir()
	executionImage := "registry.example/go-qrl@sha256:" + strings.Repeat("abcd", 16)
	runner := newTestRunner(t, Config{
		ReportDir: reports,
		Backend:   devnet.BackendDocker,
		Images:    devnet.Images{Execution: executionImage},
		Suites:    []string{"execution-console"},
	}, io.Discard, io.Discard)
	runner.networks = new(recordingNetworks)

	var sourceDir, extractedImage, extractedTool string
	runner.prepareGQRL = func(_ context.Context, mode runMode, backend devnet.Backend, testsDir, image, destination string) error {
		require.True(t, mode.provisionsNetwork())
		require.Equal(t, devnet.BackendDocker, backend)
		sourceDir, extractedImage, extractedTool = testsDir, image, destination
		return nil
	}
	runner.runCommand = func(context.Context, commandSpec) error {
		writeGinkgoReport(t, filepath.Join(reports, "lanes", executionLaneName), types.SpecStatePassed)
		return nil
	}

	require.NoError(t, runner.Run(t.Context(), executionLaneName))
	workingDirectory, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, workingDirectory, sourceDir)
	require.Equal(t, executionImage, extractedImage)
	configured, err := manifest.Read(filepath.Join(reports, "lanes", executionLaneName, manifest.FileName))
	require.NoError(t, err)
	require.Equal(t, extractedTool, configured.Tools.GQRL)
	require.NoFileExists(t, extractedTool)
	require.NoDirExists(t, filepath.Dir(extractedTool))
}

func TestRunUsesHostToolWhenCustomParametersOwnTheExecutionImage(t *testing.T) {
	reports := t.TempDir()
	runner := newTestRunner(t, Config{
		ReportDir:  reports,
		Backend:    devnet.BackendDocker,
		Parameters: []byte("custom: true"),
		Suites:     []string{"execution-console"},
	}, io.Discard, io.Discard)
	runner.networks = new(recordingNetworks)
	var configuredImage string
	runner.prepareGQRL = func(_ context.Context, mode runMode, backend devnet.Backend, _, image, _ string) error {
		require.True(t, mode.provisionsNetwork())
		require.Equal(t, devnet.BackendDocker, backend)
		configuredImage = image
		return nil
	}
	runner.runCommand = func(context.Context, commandSpec) error {
		writeGinkgoReport(t, filepath.Join(reports, "lanes", executionLaneName), types.SpecStatePassed)
		return nil
	}

	require.NoError(t, runner.Run(t.Context(), executionLaneName))
	require.Empty(t, configuredImage, "custom parameters do not expose verified execution-image provenance")
}

func TestRunClassifiesConsoleToolFailureAsInfrastructure(t *testing.T) {
	reports := t.TempDir()
	networks := new(recordingNetworks)
	runner := newTestRunner(t, Config{
		ReportDir: reports,
		Backend:   devnet.BackendDocker,
		Suites:    []string{"execution-console"},
	}, io.Discard, io.Discard)
	runner.networks = networks
	runner.prepareGQRL = func(context.Context, runMode, devnet.Backend, string, string, string) error {
		return errors.New("copy failed")
	}
	runner.runCommand = func(context.Context, commandSpec) error {
		t.Fatal("ginkgo must not run without its console tool")
		return nil
	}

	err := runner.Run(t.Context(), executionLaneName)
	require.ErrorContains(t, err, "prepare gqrl: copy failed")
	require.Equal(t, []string{devnet.DefaultEnclaveName}, networks.stopped)
	require.FileExists(t, filepath.Join(reports, "lanes", executionLaneName, "output.log"))

	summary := testutil.ReadJSON[results.Summary](t, filepath.Join(reports, results.SummaryFileName))
	require.Equal(t, results.VerdictInfrastructure, summary.Lanes[0].Verdict)
	require.Contains(t, summary.Lanes[0].Error, "copy failed")
}

func TestRunClassifiesConsoleToolCleanupFailureAsInfrastructure(t *testing.T) {
	reports := t.TempDir()
	runner := newTestRunner(t, Config{
		ReportDir: reports,
		Backend:   devnet.BackendDocker,
		Suites:    []string{"execution-console"},
	}, io.Discard, io.Discard)
	runner.networks = new(recordingNetworks)
	var toolDirectory string
	runner.prepareGQRL = func(_ context.Context, _ runMode, _ devnet.Backend, _, _, destination string) error {
		toolDirectory = filepath.Dir(destination)
		return nil
	}
	runner.removeToolDir = func(string) error { return errors.New("cleanup failed") }
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(toolDirectory)) })
	runner.runCommand = func(context.Context, commandSpec) error {
		writeGinkgoReport(t, filepath.Join(reports, "lanes", executionLaneName), types.SpecStatePassed)
		return nil
	}

	err := runner.Run(t.Context(), executionLaneName)
	require.ErrorContains(t, err, "remove tool directory: cleanup failed")
	summary := testutil.ReadJSON[results.Summary](t, filepath.Join(reports, results.SummaryFileName))
	require.Equal(t, results.VerdictInfrastructure, summary.Lanes[0].Verdict)
}

// passingCommand fakes a lane process that succeeds AND leaves a passing
// report behind — the summary treats a cleanly exited lane without a usable
// report as an infrastructure failure.
func passingCommand(t *testing.T, reports string) func(context.Context, commandSpec) error {
	t.Helper()
	return func(context.Context, commandSpec) error {
		writeGinkgoReport(t, filepath.Join(reports, "lanes", executionLaneName), types.SpecStatePassed)
		return nil
	}
}

func writeGinkgoReport(t *testing.T, laneDir string, state types.SpecState) {
	t.Helper()
	report := types.Report{SuiteSucceeded: state == types.SpecStatePassed, SpecReports: []types.SpecReport{{
		LeafNodeText: "encodes calls",
		LeafNodeType: types.NodeTypeIt,
		State:        state,
	}}}
	payload, err := json.Marshal([]types.Report{report})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(laneDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(laneDir, results.ReportFileName), payload, 0o600))
}

func outcomeErrors(outcomes []results.Outcome) []error {
	errors := make([]error, len(outcomes))
	for index, outcome := range outcomes {
		errors[index] = outcome.Err
	}
	return errors
}

func TestRunWritesRunManifestAndSummary(t *testing.T) {
	reports := t.TempDir()
	laneDir := filepath.Join(reports, "lanes", executionLaneName)
	testRunner := newTestRunner(t, Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	testRunner.networks = new(recordingNetworks)
	testRunner.runCommand = func(context.Context, commandSpec) error {
		writeGinkgoReport(t, laneDir, types.SpecStatePassed)
		return nil
	}

	require.NoError(t, testRunner.Run(t.Context(), executionLaneName))

	record := testutil.ReadJSON[runmanifest.Manifest](t, filepath.Join(reports, runmanifest.FileName))
	require.Equal(t, "passed", record.Result)
	require.Equal(t, devnet.BackendDocker, record.Backend)
	require.Equal(t, devnet.PackageLocator, record.PackageLocator)
	require.NotNil(t, record.Images)
	require.Equal(t, devnet.DefaultImages(), *record.Images)
	require.Len(t, record.Lanes, 1)
	require.Equal(t, "passed", record.Lanes[0].Result)
	require.Positive(t, record.Lanes[0].Seed)
	require.False(t, record.FinishedAt.IsZero())

	summary := testutil.ReadJSON[results.Summary](t, filepath.Join(reports, results.SummaryFileName))
	require.Equal(t, "passed", summary.Result)
	require.Equal(t, executionLaneName, summary.Lanes[0].Name)
	markdown, err := os.ReadFile(filepath.Join(reports, results.MarkdownFileName))
	require.NoError(t, err)
	require.Contains(t, string(markdown), "### execution\n")
	require.NotContains(t, string(markdown), "### execution-abi\n")
}

func TestRunFailsOnUnexpectedSkips(t *testing.T) {
	reports := t.TempDir()
	laneDir := filepath.Join(reports, "lanes", executionLaneName)
	testRunner := newTestRunner(t, Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	testRunner.networks = new(recordingNetworks)
	testRunner.runCommand = func(context.Context, commandSpec) error {
		writeGinkgoReport(t, laneDir, types.SpecStateSkipped)
		return nil
	}

	err := testRunner.Run(t.Context(), executionLaneName)
	require.ErrorContains(t, err, "execution (skipped)")

	record := testutil.ReadJSON[runmanifest.Manifest](t, filepath.Join(reports, runmanifest.FileName))
	require.Equal(t, "failed", record.Lanes[0].Result,
		"the manifest records the verdict, not the process exit")
}

func TestRunPreservesCancellation(t *testing.T) {
	reports := t.TempDir()
	laneDir := filepath.Join(reports, "lanes", executionLaneName)
	testRunner := newTestRunner(t, Config{ReportDir: reports}, io.Discard, io.Discard)
	testRunner.networks = new(recordingNetworks)
	testRunner.runCommand = func(context.Context, commandSpec) error {
		writeGinkgoReport(t, laneDir, types.SpecStateInterrupted)
		return context.Canceled
	}

	err := testRunner.Run(t.Context(), executionLaneName)
	require.ErrorIs(t, err, context.Canceled)

	summary := testutil.ReadJSON[results.Summary](t, filepath.Join(reports, results.SummaryFileName))
	require.Equal(t, results.VerdictCanceled, summary.Lanes[0].Verdict)
}

func TestRunFailsWithoutAUsableReport(t *testing.T) {
	reports := t.TempDir()
	testRunner := newTestRunner(t, Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	testRunner.networks = new(recordingNetworks)
	// The lane process "succeeds" without ever writing a report.
	testRunner.runCommand = func(context.Context, commandSpec) error { return nil }

	err := testRunner.Run(t.Context(), executionLaneName)
	require.ErrorContains(t, err, "lanes did not pass")

	record := testutil.ReadJSON[runmanifest.Manifest](t, filepath.Join(reports, runmanifest.FileName))
	require.Equal(t, "failed", record.Result)
	require.Equal(t, "failed", record.Lanes[0].Result)

	summary := testutil.ReadJSON[results.Summary](t, filepath.Join(reports, results.SummaryFileName))
	require.Equal(t, results.VerdictInfrastructure, summary.Lanes[0].Verdict)
}

func TestRunManifestSurvivesBootstrapFailure(t *testing.T) {
	reports := t.TempDir()
	networks := &recordingNetworks{startErr: errors.New("no capacity")}
	testRunner := newTestRunner(t, Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	testRunner.networks = networks

	err := testRunner.Run(t.Context(), executionLaneName)
	require.ErrorContains(t, err, "lane execution: network bootstrap failed: start network: no capacity")
	require.NoDirExists(t, filepath.Join(reports, "lanes", executionLaneName))
	require.Empty(t, networks.stopped, "a lane that never started must not be stopped")

	record := testutil.ReadJSON[runmanifest.Manifest](t, filepath.Join(reports, runmanifest.FileName))
	require.Equal(t, "failed", record.Result)
	require.Equal(t, "failed", record.Lanes[0].Result)

	summary := testutil.ReadJSON[results.Summary](t, filepath.Join(reports, results.SummaryFileName))
	require.Equal(t, results.VerdictBootstrap, summary.Lanes[0].Verdict)
}

func TestRunCollectsDiagnosticsOnFailureBeforeCleanup(t *testing.T) {
	reports := t.TempDir()
	networks := new(recordingNetworks)
	testRunner := newTestRunner(t, Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	testRunner.networks = networks
	testRunner.runCommand = func(context.Context, commandSpec) error { return errors.New("exit status 1") }

	require.Error(t, testRunner.Run(t.Context(), executionLaneName))
	diagnosticsDir := filepath.Join(reports, "lanes", executionLaneName, "diagnostics")
	require.Equal(t, []string{diagnosticsDir}, networks.collected)
	require.Equal(t, []string{"qrl-tests"}, networks.stopped, "the enclave must still be destroyed")
	require.Equal(t, diagnosticsDir, networks.started.FailureDiagnosticsDir)
}

func TestRunDiagnosticsFailureNeverMasksTheResult(t *testing.T) {
	networks := &recordingNetworks{collectErr: errors.New("logs unavailable")}
	reports := t.TempDir()
	configuration := Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}

	testRunner := newTestRunner(t, configuration, io.Discard, io.Discard)
	testRunner.networks = networks
	testRunner.runCommand = func(context.Context, commandSpec) error { return errors.New("exit status 1") }
	err := testRunner.Run(t.Context(), executionLaneName)
	require.ErrorContains(t, err, "exit status 1")
	require.ErrorContains(t, err, "collect diagnostics: logs unavailable")
	require.Equal(t, []string{"qrl-tests"}, networks.stopped)
}

func TestNewResolvesConfigurationDefaults(t *testing.T) {
	runner := newTestRunner(t, Config{}, io.Discard, io.Discard)
	require.Equal(t, ".", runner.configuration.TestsDir)
	require.Equal(t, devnet.DefaultEnclaveName, runner.configuration.BaseName)
	require.Equal(t, DefaultReportDir, runner.configuration.ReportDir)
	require.Equal(t, devnet.BackendDocker, runner.configuration.Backend)
	require.Equal(t, devnet.DefaultStartTimeout, runner.configuration.StartTimeout)
}

func TestListDescribesLanesAndSuites(t *testing.T) {
	var output bytes.Buffer
	runner := newTestRunner(t, Config{}, &output, &output)
	require.NoError(t, runner.List())
	require.Regexp(t, `(?m)^execution\s+profile=single`, output.String())
	require.Contains(t, output.String(), executionABISuite)
	require.Contains(t, output.String(), "profile=single")
	require.Contains(t, output.String(), "package=./e2e/suites/execution/abi")
}

func TestRunAllRejectsOverrides(t *testing.T) {
	for name, configuration := range map[string]Config{
		"parameters": {Parameters: []byte(`{}`)},
		"suites":     {Suites: []string{executionABISuite}},
	} {
		t.Run(name, func(t *testing.T) {
			runner := newTestRunner(t, configuration, io.Discard, io.Discard)
			require.Error(t, runner.RunAll(t.Context()))
		})
	}
}

func TestRunAllProvisionsPerLane(t *testing.T) {
	networks := new(recordingNetworks)
	var command commandSpec
	reports := t.TempDir()
	runner := newTestRunner(t, Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	runner.networks = networks
	runner.runCommand = func(_ context.Context, specification commandSpec) error {
		writeGinkgoReport(t, filepath.Join(reports, "lanes", executionLaneName), types.SpecStatePassed)
		command = specification
		return nil
	}

	require.NoError(t, runner.RunAll(t.Context()))
	require.Equal(t, "qrl-tests-execution", networks.started.EnclaveName)
	require.Equal(t, devnet.ProfileSingle, networks.started.Profile)
	require.Equal(t, []string{"qrl-tests-execution"}, networks.stopped)
	require.Contains(t, command.Args, "./e2e/suites/execution/abi")
	record := testutil.ReadJSON[runmanifest.Manifest](t, filepath.Join(reports, runmanifest.FileName))
	require.Equal(t, "qrl-tests-execution", record.Lanes[0].Enclave)
}

func TestRunReturnsCleanupFailure(t *testing.T) {
	networks := &recordingNetworks{stopErr: context.DeadlineExceeded}
	reports := t.TempDir()
	runner := newTestRunner(t, Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	runner.networks = networks
	runner.runCommand = passingCommand(t, reports)

	err := runner.Run(t.Context(), executionLaneName)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	record := testutil.ReadJSON[runmanifest.Manifest](t, filepath.Join(reports, runmanifest.FileName))
	require.Equal(t, "failed", record.Result)
	summary := testutil.ReadJSON[results.Summary](t, filepath.Join(reports, results.SummaryFileName))
	require.Equal(t, results.VerdictInfrastructure, summary.Lanes[0].Verdict)
}

func TestRunMarksManifestFailedWhenSummaryWritingFails(t *testing.T) {
	reports := t.TempDir()

	runner := newTestRunner(t, Config{ReportDir: reports}, io.Discard, io.Discard)
	runner.networks = new(recordingNetworks)
	runner.runCommand = func(ctx context.Context, specification commandSpec) error {
		require.NoError(t, passingCommand(t, reports)(ctx, specification))
		require.NoError(t, os.Mkdir(filepath.Join(reports, results.MarkdownFileName), 0o700))
		return nil
	}

	err := runner.Run(t.Context(), executionLaneName)
	require.ErrorContains(t, err, "write markdown summary")

	record := testutil.ReadJSON[runmanifest.Manifest](t, filepath.Join(reports, runmanifest.FileName))
	require.Equal(t, "failed", record.Result)
	require.Equal(t, "passed", record.Lanes[0].Result)
}

func TestClearReportArtifacts(t *testing.T) {
	reports := t.TempDir()
	owned := []string{
		filepath.Join(reports, "lanes", executionLaneName, results.ReportFileName),
		filepath.Join(reports, results.SummaryFileName),
		filepath.Join(reports, results.MarkdownFileName),
		filepath.Join(reports, runmanifest.FileName),
	}
	for _, path := range owned {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("stale"), 0o600))
	}
	unrelated := filepath.Join(reports, "keep.txt")
	require.NoError(t, os.WriteFile(unrelated, []byte("keep"), 0o600))

	require.NoError(t, clearReportArtifacts(reports))
	for _, path := range owned {
		require.NoFileExists(t, path)
	}
	require.FileExists(t, unrelated)
}

func TestAttachBuildsCommandWithoutProvisioning(t *testing.T) {
	networks := new(recordingNetworks)
	var command commandSpec
	reports := t.TempDir()
	runner := newTestRunner(t, Config{
		BaseName:  "qrl-tests",
		ReportDir: reports,
		Backend:   devnet.BackendDocker,
	}, io.Discard, io.Discard)
	runner.networks = networks
	var toolMode runMode
	var toolImage string
	runner.prepareGQRL = func(_ context.Context, mode runMode, _ devnet.Backend, _, image, destination string) error {
		toolMode, toolImage = mode, image
		return os.WriteFile(destination, []byte("gqrl"), 0o755)
	}
	runner.runCommand = func(_ context.Context, specification commandSpec) error {
		writeGinkgoReport(t, filepath.Join(reports, "lanes", executionLaneName), types.SpecStatePassed)
		command = specification
		return nil
	}

	require.NoError(t, runner.Test(t.Context(), executionLaneName))
	require.Equal(t, "qrl-tests", networks.inspected)
	require.Empty(t, networks.started.EnclaveName, "attaching must not provision")
	require.Empty(t, networks.stopped, "attaching must not stop the network")
	require.Equal(t, useExistingNetwork, toolMode)
	require.Empty(t, toolImage, "attached networks have no verified image provenance")
	require.Contains(t, command.Args, "./e2e/suites/execution/abi")
}

func TestAttachRejectsCustomParameters(t *testing.T) {
	runner := newTestRunner(t, Config{Parameters: []byte(`{}`)}, io.Discard, io.Discard)
	require.ErrorContains(t, runner.Test(t.Context(), executionLaneName), "existing network")
}

func testLaneRuns(t *testing.T, reports string, count int) runPlan {
	t.Helper()
	lane, err := lanes.Named(executionLaneName)
	require.NoError(t, err)

	laneRuns := make([]laneRun, count)
	for index := range laneRuns {
		name := fmt.Sprintf("lane-%d", index)
		reportDir := filepath.Join(reports, name)
		laneRuns[index] = laneRun{
			definition:  lane,
			enclaveName: name,
			reportDir:   reportDir,
		}
	}
	return runPlan{testsDir: ".", reportRoot: reports, mode: provisionNetwork, lanes: laneRuns}
}

func TestRunLanesRunsConcurrently(t *testing.T) {
	networks := new(recordingNetworks)
	runner := newTestRunner(t, Config{MaxParallel: 2}, io.Discard, io.Discard)
	runner.networks = networks
	runner.runCommand = func(context.Context, commandSpec) error { return nil }

	planned := testLaneRuns(t, t.TempDir(), 2)
	require.NoError(t, errors.Join(outcomeErrors(runner.runLanes(t.Context(), planned))...))
	require.ElementsMatch(t, []string{"lane-0", "lane-1"}, networks.stopped)
}

func TestRunLanesHonorsCancellation(t *testing.T) {
	networks := new(recordingNetworks)
	runner := newTestRunner(t, Config{MaxParallel: 2}, io.Discard, io.Discard)
	runner.networks = networks
	entered := make(chan struct{}, 3)
	runner.runCommand = func(ctx context.Context, _ commandSpec) error {
		entered <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}

	// Three lanes against two slots: two block inside the command, the third
	// waits on the semaphore until a canceled lane releases its slot and then
	// fails through runLane itself.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	planned := testLaneRuns(t, t.TempDir(), 3)
	done := make(chan error, 1)
	go func() { done <- errors.Join(outcomeErrors(runner.runLanes(ctx, planned))...) }()

	<-entered
	<-entered
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestPlanLanesDescribesEachLane(t *testing.T) {
	reports := t.TempDir()
	execution, err := lanes.Named(executionLaneName)
	require.NoError(t, err)
	selected := []lanes.Lane{execution}
	plan, err := planLanes(Config{BaseName: "qrl-tests", ReportDir: reports}.withDefaults(), selected, provisionPerLane)
	require.NoError(t, err)
	require.Equal(t, reports, plan.reportRoot)
	require.Len(t, plan.lanes, 1)
	laneRun := plan.lanes[0]
	require.Equal(t, "qrl-tests-execution", laneRun.enclaveName)
	require.Equal(t, filepath.Join(reports, laneReportsDirectory, "execution", manifest.FileName), laneRun.manifestPath())
	require.Contains(t, laneRun.ginkgoArguments(), "./e2e/suites/execution/abi")
	require.Contains(t, laneRun.ginkgoArguments(), fmt.Sprintf("--seed=%d", laneRun.seed))
	require.Positive(t, laneRun.seed)
	require.True(t, plan.mode.provisionsNetwork())
}

func TestExecuteWiresTheCommand(t *testing.T) {
	gopath := t.TempDir()
	var output bytes.Buffer
	err := execute(t.Context(), commandSpec{
		Path:   "go",
		Args:   []string{"env", "GOPATH"},
		Env:    append(os.Environ(), "GOPATH="+gopath),
		Stdout: &output,
		Stderr: io.Discard,
	})
	require.NoError(t, err)
	require.Equal(t, gopath, strings.TrimSpace(output.String()))
}

func testEnvironment(name string, backend devnet.Backend) devnet.Environment {
	return devnet.Environment{
		EnclaveName: name,
		Backend:     backend,
		Participants: []devnet.Participant{{
			Index: 1,
			Execution: devnet.ExecutionService{
				RPCURL: "http://127.0.0.1:8545",
			},
			Consensus: devnet.ConsensusService{URL: "http://127.0.0.1:3500"},
		}},
	}
}
