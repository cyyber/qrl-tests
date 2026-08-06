package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/lanes"
	"github.com/cyyber/qrl-tests/e2e/internal/runenv"
	"github.com/stretchr/testify/require"
)

type recordingNetworks struct {
	mutex     sync.Mutex
	started   devnet.StartOptions
	inspected string
	stopped   []string
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

func TestRunBuildsCommandAndCleansUp(t *testing.T) {
	reports := t.TempDir()
	networks := new(recordingNetworks)
	var command commandSpec
	var output bytes.Buffer
	tests := New(Config{
		BaseName:     "qrl-tests",
		ReportDir:    reports,
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
		Parameters:   []byte(`{"custom":true}`),
		Suites:       []string{"execution-abi"},
	}, &output, &output)
	tests.networks = networks
	tests.runCommand = func(_ context.Context, specification commandSpec) error {
		command = specification
		return nil
	}

	require.NoError(t, tests.Run(t.Context(), "execution-abi"))
	require.Equal(t, "qrl-tests", networks.started.EnclaveName)
	require.Equal(t, devnet.ProfileSingle, networks.started.Profile)
	require.Equal(t, []byte(`{"custom":true}`), networks.started.Parameters)
	require.Equal(t, []string{"qrl-tests"}, networks.stopped)
	require.Equal(t, "go", command.Path)
	require.Contains(t, command.Args, "./e2e/suites/execution/abi")

	manifestPath := filepath.Join(reports, "execution-abi", "environment.json")
	manifest, err := runenv.Read(manifestPath)
	require.NoError(t, err)
	require.Equal(t, "execution-abi", manifest.Lane)
	require.Equal(t, devnet.ProfileSingle, manifest.Profile)
	require.Contains(t, command.Env, runenv.PathEnv+"="+manifestPath)
	logs, err := filepath.Glob(filepath.Join(reports, "execution-abi", "output.log"))
	require.NoError(t, err)
	require.Len(t, logs, 1)
}

func TestListDescribesLanesAndSuites(t *testing.T) {
	var output bytes.Buffer
	tests := New(Config{}, &output, &output)
	require.NoError(t, tests.List())
	require.Contains(t, output.String(), "execution-abi")
	require.Contains(t, output.String(), "profile=single")
}

func TestRunAllRejectsOverrides(t *testing.T) {
	for name, configuration := range map[string]Config{
		"parameters": {Parameters: []byte(`{}`)},
		"suites":     {Suites: []string{"execution-abi"}},
	} {
		t.Run(name, func(t *testing.T) {
			tests := New(configuration, nil, nil)
			require.Error(t, tests.RunAll(t.Context()))
		})
	}
}

func TestRunReturnsCleanupFailure(t *testing.T) {
	networks := &recordingNetworks{stopErr: errors.New("stop failed")}
	tests := New(Config{
		BaseName:     "qrl-tests",
		ReportDir:    t.TempDir(),
		Backend:      devnet.BackendDocker,
		StartTimeout: time.Minute,
	}, nil, nil)
	tests.networks = networks
	tests.runCommand = func(context.Context, commandSpec) error { return nil }

	err := tests.Run(t.Context(), "execution-abi")
	require.ErrorContains(t, err, "stop failed")
}

func TestTestAttachesToExistingNetwork(t *testing.T) {
	networks := new(recordingNetworks)
	var command commandSpec
	tests := New(Config{
		BaseName:  "qrl-tests",
		ReportDir: t.TempDir(),
		Backend:   devnet.BackendDocker,
	}, nil, nil)
	tests.networks = networks
	tests.runCommand = func(_ context.Context, specification commandSpec) error {
		command = specification
		return nil
	}

	require.NoError(t, tests.Test(t.Context(), "execution-abi"))
	require.Equal(t, "qrl-tests", networks.inspected)
	require.Empty(t, networks.started.EnclaveName, "attaching must not provision")
	require.Empty(t, networks.stopped, "attaching must not stop the network")
	require.Contains(t, command.Args, "./e2e/suites/execution/abi")
}

func TestTestRejectsCustomParameters(t *testing.T) {
	tests := New(Config{Parameters: []byte(`{}`)}, nil, nil)
	require.ErrorContains(t, tests.Test(t.Context(), "execution-abi"), "existing network")
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
			manifestPath: filepath.Join(reportDir, "environment.json"),
			provision:    true,
		}
	}
	return planned
}

func TestRunLanesRunsConcurrently(t *testing.T) {
	networks := new(recordingNetworks)
	tests := New(Config{MaxParallel: 2}, nil, nil)
	tests.networks = networks
	tests.runCommand = func(context.Context, commandSpec) error { return nil }

	planned := testLaneRuns(t, t.TempDir(), 2)
	require.NoError(t, tests.runLanes(t.Context(), planned))
	require.ElementsMatch(t, []string{"lane-0", "lane-1"}, networks.stopped)
}

func TestRunLanesHonorsCancellation(t *testing.T) {
	networks := new(recordingNetworks)
	tests := New(Config{MaxParallel: 2}, nil, nil)
	tests.networks = networks
	entered := make(chan struct{}, 3)
	tests.runCommand = func(ctx context.Context, _ commandSpec) error {
		entered <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}

	// Three lanes against two slots: two block inside the command, the third
	// waits on the semaphore until cancellation aborts all of them.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	planned := testLaneRuns(t, t.TempDir(), 3)
	done := make(chan error, 1)
	go func() { done <- tests.runLanes(ctx, planned) }()

	<-entered
	<-entered
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestRunPlanDescribesEachLane(t *testing.T) {
	reports := t.TempDir()
	executionABI, err := lanes.Named("execution-abi")
	require.NoError(t, err)
	selected := []lanes.Lane{executionABI}
	plan, err := newRunPlan(Config{BaseName: "qrl-tests", ReportDir: reports}, selected, provisionPerLane)
	require.NoError(t, err)
	require.Len(t, plan.lanes, 1)
	require.Equal(t, "qrl-tests-execution-abi", plan.lanes[0].enclaveName)
	require.Equal(t, filepath.Join(reports, "execution-abi", "environment.json"), plan.lanes[0].manifestPath)
	require.Contains(t, plan.lanes[0].arguments, "./e2e/suites/execution/abi")
	require.True(t, plan.lanes[0].provision)
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
