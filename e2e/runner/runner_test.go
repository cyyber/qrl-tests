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
	"sync"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/lanes"
	"github.com/cyyber/qrl-tests/e2e/internal/manifest"
	"github.com/cyyber/qrl-tests/internal/results"
	"github.com/cyyber/qrl-tests/internal/runmanifest"
	"github.com/onsi/ginkgo/v2/types"
	"github.com/stretchr/testify/require"
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

func captureCommand(command *commandSpec) func(context.Context, commandSpec) error {
	return func(_ context.Context, specification commandSpec) error {
		*command = specification
		return nil
	}
}

func (networks *recordingNetworks) Collect(_ context.Context, enclave, outputDir string) error {
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
	runner := New(Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
		Parameters:   []byte(`{"custom":true}`),
		Suites:       []string{"execution-abi"},
	}, &output, &output)
	runner.networks = networks
	runner.runCommand = func(ctx context.Context, spec commandSpec) error {
		writeGinkgoReport(t, filepath.Join(reports, "lanes", "execution-abi"), types.SpecStatePassed)
		return captureCommand(&command)(ctx, spec)
	}

	require.NoError(t, runner.Run(t.Context(), "execution-abi"))
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

	manifestPath := filepath.Join(reports, "lanes", "execution-abi", manifest.FileName)
	written, err := manifest.Read(manifestPath)
	require.NoError(t, err)
	require.Equal(t, "execution-abi", written.Lane)
	require.Equal(t, devnet.ProfileSingle, written.Profile)
	require.Equal(t, testEnvironment("qrl-tests", devnet.BackendDocker), written.Environment)
	require.Contains(t, command.Env, manifest.PathEnv+"="+manifestPath)
	require.FileExists(t, filepath.Join(reports, "lanes", "execution-abi", "output.log"))
	require.Contains(t, output.String(), "=== RUN lane=execution-abi profile=single ===")

	recordPath := filepath.Join(reports, runmanifest.FileName)
	record := readRunManifest(t, recordPath)
	require.Equal(t, "c0b29628173dba03445f2a6b7f07aa6b5958f93af975feefff9ee025d4cc0c10", record.ParametersSHA256)
	payload, err := os.ReadFile(recordPath)
	require.NoError(t, err)
	require.NotContains(t, string(payload), `"custom_parameters":`)
}

// passingCommand fakes a lane process that succeeds AND leaves a passing
// report behind — the summary treats a cleanly exited lane without a usable
// report as an infrastructure failure.
func passingCommand(t *testing.T, reports string) func(context.Context, commandSpec) error {
	t.Helper()
	return func(context.Context, commandSpec) error {
		writeGinkgoReport(t, filepath.Join(reports, "lanes", "execution-abi"), types.SpecStatePassed)
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

func readRunManifest(t *testing.T, path string) runmanifest.Manifest {
	t.Helper()
	payload, err := os.ReadFile(path)
	require.NoError(t, err)
	var record runmanifest.Manifest
	require.NoError(t, json.Unmarshal(payload, &record))
	return record
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
	laneDir := filepath.Join(reports, "lanes", "execution-abi")
	testRunner := New(Config{
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

	require.NoError(t, testRunner.Run(t.Context(), "execution-abi"))

	record := readRunManifest(t, filepath.Join(reports, runmanifest.FileName))
	require.Equal(t, "passed", record.Result)
	require.Equal(t, devnet.BackendDocker, record.Backend)
	require.Equal(t, devnet.PackageLocator, record.PackageLocator)
	require.NotNil(t, record.Images)
	require.Equal(t, devnet.DefaultImages(), *record.Images)
	require.Len(t, record.Lanes, 1)
	require.Equal(t, "passed", record.Lanes[0].Result)
	require.Positive(t, record.Lanes[0].Seed)
	require.False(t, record.FinishedAt.IsZero())

	payload, err := os.ReadFile(filepath.Join(reports, results.SummaryFileName))
	require.NoError(t, err)
	var summary results.Summary
	require.NoError(t, json.Unmarshal(payload, &summary))
	require.Equal(t, "passed", summary.Result)
	require.FileExists(t, filepath.Join(reports, results.MarkdownFileName))
}

func TestRunFailsOnUnexpectedSkips(t *testing.T) {
	reports := t.TempDir()
	laneDir := filepath.Join(reports, "lanes", "execution-abi")
	testRunner := New(Config{
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

	err := testRunner.Run(t.Context(), "execution-abi")
	require.ErrorContains(t, err, "execution-abi (skipped)")

	record := readRunManifest(t, filepath.Join(reports, runmanifest.FileName))
	require.Equal(t, "failed", record.Lanes[0].Result,
		"the manifest records the verdict, not the process exit")
}

func TestRunPreservesCancellation(t *testing.T) {
	reports := t.TempDir()
	laneDir := filepath.Join(reports, "lanes", "execution-abi")
	testRunner := New(Config{ReportDir: reports}, io.Discard, io.Discard)
	testRunner.networks = new(recordingNetworks)
	testRunner.runCommand = func(context.Context, commandSpec) error {
		writeGinkgoReport(t, laneDir, types.SpecStateInterrupted)
		return context.Canceled
	}

	err := testRunner.Run(t.Context(), "execution-abi")
	require.ErrorIs(t, err, context.Canceled)

	payload, readErr := os.ReadFile(filepath.Join(reports, results.SummaryFileName))
	require.NoError(t, readErr)
	var summary results.Summary
	require.NoError(t, json.Unmarshal(payload, &summary))
	require.Equal(t, results.VerdictCanceled, summary.Lanes[0].Verdict)
}

func TestRunFailsWithoutAUsableReport(t *testing.T) {
	reports := t.TempDir()
	testRunner := New(Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	testRunner.networks = new(recordingNetworks)
	// The lane process "succeeds" without ever writing a report.
	testRunner.runCommand = func(context.Context, commandSpec) error { return nil }

	err := testRunner.Run(t.Context(), "execution-abi")
	require.ErrorContains(t, err, "lanes did not pass")

	record := readRunManifest(t, filepath.Join(reports, runmanifest.FileName))
	require.Equal(t, "failed", record.Result)
	require.Equal(t, "failed", record.Lanes[0].Result)

	payload, readErr := os.ReadFile(filepath.Join(reports, results.SummaryFileName))
	require.NoError(t, readErr)
	var summary results.Summary
	require.NoError(t, json.Unmarshal(payload, &summary))
	require.Equal(t, results.VerdictInfrastructure, summary.Lanes[0].Verdict)
}

func TestRunManifestSurvivesBootstrapFailure(t *testing.T) {
	reports := t.TempDir()
	networks := &recordingNetworks{startErr: errors.New("no capacity")}
	testRunner := New(Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	testRunner.networks = networks

	err := testRunner.Run(t.Context(), "execution-abi")
	require.ErrorContains(t, err, "lane execution-abi: network bootstrap failed: start network: no capacity")
	require.NoDirExists(t, filepath.Join(reports, "lanes", "execution-abi"))
	require.Empty(t, networks.stopped, "a lane that never started must not be stopped")

	record := readRunManifest(t, filepath.Join(reports, runmanifest.FileName))
	require.Equal(t, "failed", record.Result)
	require.Equal(t, "failed", record.Lanes[0].Result)

	payload, err := os.ReadFile(filepath.Join(reports, results.SummaryFileName))
	require.NoError(t, err)
	var summary results.Summary
	require.NoError(t, json.Unmarshal(payload, &summary))
	require.Equal(t, results.VerdictBootstrap, summary.Lanes[0].Verdict)
}

func TestRunCollectsDiagnosticsOnFailureBeforeCleanup(t *testing.T) {
	reports := t.TempDir()
	networks := new(recordingNetworks)
	testRunner := New(Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	testRunner.networks = networks
	testRunner.runCommand = func(context.Context, commandSpec) error { return errors.New("exit status 1") }

	require.Error(t, testRunner.Run(t.Context(), "execution-abi"))
	diagnosticsDir := filepath.Join(reports, "lanes", "execution-abi", "diagnostics")
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

	testRunner := New(configuration, io.Discard, io.Discard)
	testRunner.networks = networks
	testRunner.runCommand = func(context.Context, commandSpec) error { return errors.New("exit status 1") }
	err := testRunner.Run(t.Context(), "execution-abi")
	require.ErrorContains(t, err, "exit status 1")
	require.ErrorContains(t, err, "collect diagnostics: logs unavailable")
	require.Equal(t, []string{"qrl-tests"}, networks.stopped)
}

func TestNewResolvesConfigurationDefaults(t *testing.T) {
	runner := New(Config{}, io.Discard, io.Discard)
	require.Equal(t, ".", runner.configuration.TestsDir)
	require.Equal(t, devnet.DefaultEnclaveName, runner.configuration.BaseName)
	require.Equal(t, DefaultReportDir, runner.configuration.ReportDir)
	require.Equal(t, devnet.BackendDocker, runner.configuration.Backend)
	require.Equal(t, devnet.DefaultStartTimeout, runner.configuration.StartTimeout)
}

func TestListDescribesLanesAndSuites(t *testing.T) {
	var output bytes.Buffer
	runner := New(Config{}, &output, &output)
	require.NoError(t, runner.List())
	require.Contains(t, output.String(), "execution-abi")
	require.Contains(t, output.String(), "profile=single")
	require.Contains(t, output.String(), "package=./e2e/suites/execution/abi")
}

func TestRunAllRejectsOverrides(t *testing.T) {
	for name, configuration := range map[string]Config{
		"parameters": {Parameters: []byte(`{}`)},
		"suites":     {Suites: []string{"execution-abi"}},
	} {
		t.Run(name, func(t *testing.T) {
			runner := New(configuration, io.Discard, io.Discard)
			require.Error(t, runner.RunAll(t.Context()))
		})
	}
}

func TestRunAllProvisionsPerLane(t *testing.T) {
	networks := new(recordingNetworks)
	var command commandSpec
	reports := t.TempDir()
	runner := New(Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	runner.networks = networks
	runner.runCommand = func(ctx context.Context, spec commandSpec) error {
		writeGinkgoReport(t, filepath.Join(reports, "lanes", "execution-abi"), types.SpecStatePassed)
		return captureCommand(&command)(ctx, spec)
	}

	require.NoError(t, runner.RunAll(t.Context()))
	require.Equal(t, "qrl-tests-execution-abi", networks.started.EnclaveName)
	require.Equal(t, devnet.ProfileSingle, networks.started.Profile)
	require.Equal(t, []string{"qrl-tests-execution-abi"}, networks.stopped)
	require.Contains(t, command.Args, "./e2e/suites/execution/abi")
	record := readRunManifest(t, filepath.Join(reports, runmanifest.FileName))
	require.Equal(t, "qrl-tests-execution-abi", record.Lanes[0].Enclave)
}

func TestRunReturnsCleanupFailure(t *testing.T) {
	networks := &recordingNetworks{stopErr: context.DeadlineExceeded}
	reports := t.TempDir()
	runner := New(Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	runner.networks = networks
	runner.runCommand = passingCommand(t, reports)

	err := runner.Run(t.Context(), "execution-abi")
	require.ErrorIs(t, err, context.DeadlineExceeded)

	record := readRunManifest(t, filepath.Join(reports, runmanifest.FileName))
	require.Equal(t, "failed", record.Result)
	payload, readErr := os.ReadFile(filepath.Join(reports, results.SummaryFileName))
	require.NoError(t, readErr)
	var summary results.Summary
	require.NoError(t, json.Unmarshal(payload, &summary))
	require.Equal(t, results.VerdictInfrastructure, summary.Lanes[0].Verdict)
}

func TestRunMarksManifestFailedWhenSummaryWritingFails(t *testing.T) {
	reports := t.TempDir()

	runner := New(Config{ReportDir: reports}, io.Discard, io.Discard)
	runner.networks = new(recordingNetworks)
	runner.runCommand = func(ctx context.Context, specification commandSpec) error {
		require.NoError(t, passingCommand(t, reports)(ctx, specification))
		require.NoError(t, os.Mkdir(filepath.Join(reports, results.MarkdownFileName), 0o700))
		return nil
	}

	err := runner.Run(t.Context(), "execution-abi")
	require.ErrorContains(t, err, "write markdown summary")

	record := readRunManifest(t, filepath.Join(reports, runmanifest.FileName))
	require.Equal(t, "failed", record.Result)
	require.Equal(t, "passed", record.Lanes[0].Result)
}

func TestClearReportArtifacts(t *testing.T) {
	reports := t.TempDir()
	owned := []string{
		filepath.Join(reports, "lanes", "execution", results.ReportFileName),
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
	runner := New(Config{
		BaseName:  "qrl-tests",
		ReportDir: reports,
		Backend:   devnet.BackendDocker,
	}, io.Discard, io.Discard)
	runner.networks = networks
	runner.runCommand = func(ctx context.Context, spec commandSpec) error {
		writeGinkgoReport(t, filepath.Join(reports, "lanes", "execution-abi"), types.SpecStatePassed)
		return captureCommand(&command)(ctx, spec)
	}

	require.NoError(t, runner.Test(t.Context(), "execution-abi"))
	require.Equal(t, "qrl-tests", networks.inspected)
	require.Empty(t, networks.started.EnclaveName, "attaching must not provision")
	require.Empty(t, networks.stopped, "attaching must not stop the network")
	require.Contains(t, command.Args, "./e2e/suites/execution/abi")
}

func TestAttachRejectsCustomParameters(t *testing.T) {
	runner := New(Config{Parameters: []byte(`{}`)}, io.Discard, io.Discard)
	require.ErrorContains(t, runner.Test(t.Context(), "execution-abi"), "existing network")
}

func testLaneRuns(t *testing.T, reports string, count int) runPlan {
	t.Helper()
	lane, err := lanes.Named("execution-abi")
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
	runner := New(Config{MaxParallel: 2}, io.Discard, io.Discard)
	runner.networks = networks
	runner.runCommand = func(context.Context, commandSpec) error { return nil }

	planned := testLaneRuns(t, t.TempDir(), 2)
	require.NoError(t, errors.Join(outcomeErrors(runner.runLanes(t.Context(), planned))...))
	require.ElementsMatch(t, []string{"lane-0", "lane-1"}, networks.stopped)
}

func TestRunLanesHonorsCancellation(t *testing.T) {
	networks := new(recordingNetworks)
	runner := New(Config{MaxParallel: 2}, io.Discard, io.Discard)
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
	executionABI, err := lanes.Named("execution-abi")
	require.NoError(t, err)
	selected := []lanes.Lane{executionABI}
	plan, err := planLanes(Config{BaseName: "qrl-tests", ReportDir: reports}.withDefaults(), selected, provisionPerLane)
	require.NoError(t, err)
	require.Equal(t, reports, plan.reportRoot)
	require.Len(t, plan.lanes, 1)
	laneRun := plan.lanes[0]
	require.Equal(t, "qrl-tests-execution-abi", laneRun.enclaveName)
	require.Equal(t, filepath.Join(reports, laneReportsDirectory, "execution-abi", manifest.FileName), laneRun.manifestPath())
	require.Contains(t, laneRun.ginkgoArguments(), "./e2e/suites/execution/abi")
	require.Contains(t, laneRun.ginkgoArguments(), fmt.Sprintf("--seed=%d", laneRun.seed))
	require.Positive(t, laneRun.seed)
	require.True(t, plan.mode.provisionsNetwork())
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
