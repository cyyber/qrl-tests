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
	"strings"
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
	mutex     sync.Mutex
	started   devnet.StartOptions
	inspected string
	stopped   []string
	startErr  error
	stopErr   error
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

func TestRunBuildsCommandAndCleansUp(t *testing.T) {
	reports := t.TempDir()
	networks := new(recordingNetworks)
	var command commandSpec
	var output bytes.Buffer
	runner := New(Config{
		BaseName:       "qrl-tests",
		ReportDir:      reports,
		Backend:        devnet.BackendDocker,
		PackageLocator: "github.com/rgeraldes24/qrl-package@0000000000000000000000000000000000000000",
		StartTimeout:   time.Minute,
		Parameters:     []byte(`{"custom":true}`),
		Suites:         []string{"execution-abi"},
	}, &output, &output)
	runner.networks = networks
	runner.runCommand = captureCommand(&command)

	require.NoError(t, runner.Run(t.Context(), "execution-abi"))
	require.Equal(t, "qrl-tests", networks.started.EnclaveName)
	require.Equal(t, devnet.ProfileSingle, networks.started.Profile)
	require.Equal(t, []byte(`{"custom":true}`), networks.started.Parameters)
	require.Equal(t, "github.com/rgeraldes24/qrl-package@0000000000000000000000000000000000000000", networks.started.PackageLocator)
	require.Equal(t, []string{"qrl-tests"}, networks.stopped)
	require.Equal(t, "go", command.Path)
	require.Contains(t, command.Args, "./e2e/suites/execution/abi")
	workingDirectory, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, workingDirectory, command.Dir)
	require.Contains(t, command.Env, "PATH="+os.Getenv("PATH"))

	manifestPath := filepath.Join(reports, "lanes", "execution-abi", "manifest.json")
	written, err := manifest.Read(manifestPath)
	require.NoError(t, err)
	require.Equal(t, "execution-abi", written.Lane)
	require.Equal(t, devnet.ProfileSingle, written.Profile)
	require.Equal(t, testEnvironment("qrl-tests", devnet.BackendDocker), written.Environment)
	require.Contains(t, command.Env, manifest.PathEnv+"="+manifestPath)
	require.FileExists(t, filepath.Join(reports, "lanes", "execution-abi", "output.log"))
	require.Contains(t, output.String(), "=== RUN lane=execution-abi profile=single ===")
}

func writeGinkgoReport(t *testing.T, laneDir string, state types.SpecState) {
	t.Helper()
	report := types.Report{SpecReports: []types.SpecReport{{
		LeafNodeText: "encodes calls",
		LeafNodeType: types.NodeTypeIt,
		State:        state,
	}}}
	payload, err := json.Marshal([]types.Report{report})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(laneDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(laneDir, "report.json"), payload, 0o600))
}

func TestRunWritesRunManifestAndSummary(t *testing.T) {
	reports := t.TempDir()
	laneDir := filepath.Join(reports, "lanes", "execution-abi")
	tests := New(Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	tests.networks = new(recordingNetworks)
	tests.runCommand = func(context.Context, commandSpec) error {
		writeGinkgoReport(t, laneDir, types.SpecStatePassed)
		return nil
	}

	require.NoError(t, tests.Run(t.Context(), "execution-abi"))

	record, err := runmanifest.Read(filepath.Join(reports, runmanifest.FileName))
	require.NoError(t, err)
	require.Equal(t, "passed", record.Result)
	require.Equal(t, devnet.BackendDocker, record.Backend)
	require.Equal(t, devnet.DefaultPackageLocator, record.PackageLocator)
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
	tests := New(Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	tests.networks = new(recordingNetworks)
	tests.runCommand = func(context.Context, commandSpec) error {
		writeGinkgoReport(t, laneDir, types.SpecStateSkipped)
		return nil
	}

	err := tests.Run(t.Context(), "execution-abi")
	require.ErrorContains(t, err, "unexpected skipped or pending specs")

	record, readErr := runmanifest.Read(filepath.Join(reports, runmanifest.FileName))
	require.NoError(t, readErr)
	require.Equal(t, "passed", record.Lanes[0].Result,
		"the lane process succeeded; the skip verdict comes from the summary")
}

func TestRunManifestSurvivesBootstrapFailure(t *testing.T) {
	reports := t.TempDir()
	tests := New(Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	tests.networks = &recordingNetworks{startErr: errors.New("no capacity")}

	require.Error(t, tests.Run(t.Context(), "execution-abi"))

	record, err := runmanifest.Read(filepath.Join(reports, runmanifest.FileName))
	require.NoError(t, err)
	require.Equal(t, "failed", record.Result)
	require.Equal(t, "failed", record.Lanes[0].Result)

	payload, err := os.ReadFile(filepath.Join(reports, results.SummaryFileName))
	require.NoError(t, err)
	var summary results.Summary
	require.NoError(t, json.Unmarshal(payload, &summary))
	require.Equal(t, results.ClassBootstrap, summary.Lanes[0].Class)
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
	runner := New(Config{
		BaseName:     "qrl-tests",
		ReportDir:    t.TempDir(),
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	runner.networks = networks
	runner.runCommand = captureCommand(&command)

	require.NoError(t, runner.RunAll(t.Context()))
	require.Equal(t, "qrl-tests-execution-abi", networks.started.EnclaveName)
	require.Equal(t, devnet.ProfileSingle, networks.started.Profile)
	require.Equal(t, []string{"qrl-tests-execution-abi"}, networks.stopped)
	require.Contains(t, command.Args, "./e2e/suites/execution/abi")
}

func TestRunReturnsCleanupFailure(t *testing.T) {
	networks := &recordingNetworks{stopErr: errors.New("stop failed")}
	runner := New(Config{
		BaseName:     "qrl-tests",
		ReportDir:    t.TempDir(),
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	runner.networks = networks
	runner.runCommand = func(context.Context, commandSpec) error { return nil }

	err := runner.Run(t.Context(), "execution-abi")
	require.ErrorContains(t, err, "lane execution-abi: stop network: stop failed")
}

func TestRunLeavesNoArtifactsWhenStartFails(t *testing.T) {
	reports := t.TempDir()
	networks := &recordingNetworks{startErr: errors.New("no capacity")}
	runner := New(Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, io.Discard, io.Discard)
	runner.networks = networks

	err := runner.Run(t.Context(), "execution-abi")
	require.ErrorContains(t, err, "lane execution-abi: network bootstrap failed: start network: no capacity")
	require.NoDirExists(t, filepath.Join(reports, "lanes", "execution-abi"))
	require.Empty(t, networks.stopped, "a lane that never started must not be stopped")
}

func TestAttachBuildsCommandWithoutProvisioning(t *testing.T) {
	networks := new(recordingNetworks)
	var command commandSpec
	runner := New(Config{
		BaseName:  "qrl-tests",
		ReportDir: t.TempDir(),
		Backend:   devnet.BackendDocker,
	}, io.Discard, io.Discard)
	runner.networks = networks
	runner.runCommand = captureCommand(&command)

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

func testLaneRuns(t *testing.T, reports string, count int) []laneRun {
	t.Helper()
	lane, err := lanes.Named("execution-abi")
	require.NoError(t, err)

	planned := make([]laneRun, count)
	for index := range planned {
		name := fmt.Sprintf("lane-%d", index)
		reportDir := filepath.Join(reports, name)
		planned[index] = laneRun{
			lane:         lane,
			enclaveName:  name,
			reportDir:    reportDir,
			manifestPath: filepath.Join(reportDir, "manifest.json"),
			provision:    true,
		}
	}
	return planned
}

func TestRunLanesRunsConcurrently(t *testing.T) {
	networks := new(recordingNetworks)
	runner := New(Config{MaxParallel: 2}, io.Discard, io.Discard)
	runner.networks = networks
	runner.runCommand = func(context.Context, commandSpec) error { return nil }

	planned := testLaneRuns(t, t.TempDir(), 2)
	require.NoError(t, errors.Join(runner.runLanes(t.Context(), planned)...))
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
	go func() { done <- errors.Join(runner.runLanes(ctx, planned)...) }()

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
	planned, reportRoot, err := planLanes(Config{BaseName: "qrl-tests", ReportDir: reports}, selected, provisionPerLane)
	require.NoError(t, err)
	require.Equal(t, reports, reportRoot)
	require.Len(t, planned, 1)
	require.Equal(t, "qrl-tests-execution-abi", planned[0].enclaveName)
	require.Equal(t, filepath.Join(reports, "lanes", "execution-abi", "manifest.json"), planned[0].manifestPath)
	require.Contains(t, planned[0].arguments, "./e2e/suites/execution/abi")
	require.Contains(t, planned[0].arguments, fmt.Sprintf("--seed=%d", planned[0].seed))
	require.Positive(t, planned[0].seed)
	require.True(t, planned[0].provision)
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
