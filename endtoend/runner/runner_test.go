// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

package runner

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/endtoend/internal/lanes"
	"github.com/cyyber/qrl-tests/endtoend/internal/runenv"
	"github.com/stretchr/testify/require"
)

type recordingNetworks struct {
	started devnet.StartOptions
	stopped string
	stopErr error
}

func (networks *recordingNetworks) Start(_ context.Context, options devnet.StartOptions) (devnet.Environment, error) {
	networks.started = options
	return testEnvironment(options.EnclaveName, options.Backend), nil
}

func (networks *recordingNetworks) Inspect(_ context.Context, name string, backend devnet.Backend) (devnet.Environment, error) {
	return testEnvironment(name, backend), nil
}

func (networks *recordingNetworks) Stop(_ context.Context, name string) error {
	networks.stopped = name
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

	require.NoError(t, tests.Run(t.Context(), "single"))
	require.Equal(t, "qrl-tests", networks.started.EnclaveName)
	require.Equal(t, devnet.ProfileSingle, networks.started.Profile)
	require.Equal(t, []byte(`{"custom":true}`), networks.started.Parameters)
	require.Equal(t, "qrl-tests", networks.stopped)
	require.Equal(t, "go", command.Path)
	require.Contains(t, command.Args, "./endtoend/suites/execution/abi")

	manifestPath := filepath.Join(reports, "single", "environment.json")
	manifest, err := runenv.Read(manifestPath)
	require.NoError(t, err)
	require.Equal(t, "single", manifest.Lane)
	require.Equal(t, devnet.ProfileSingle, manifest.Profile)
	require.Contains(t, command.Env, runenv.PathEnv+"="+manifestPath)
	logs, err := filepath.Glob(filepath.Join(reports, "single", "output.log"))
	require.NoError(t, err)
	require.Len(t, logs, 1)
}

func TestListDescribesLanesAndSuites(t *testing.T) {
	var output bytes.Buffer
	tests := New(Config{}, &output, &output)
	require.NoError(t, tests.List())
	require.Contains(t, output.String(), "single")
	require.Contains(t, output.String(), "execution-abi")
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

	err := tests.Run(t.Context(), "single")
	require.ErrorContains(t, err, "stop failed")
}

func TestRunPlanDescribesEachLane(t *testing.T) {
	reports := t.TempDir()
	single, err := lanes.Named("single")
	require.NoError(t, err)
	selected := []lanes.Lane{single}
	plan, err := newRunPlan(Config{BaseName: "qrl-tests", ReportDir: reports}, selected, provisionPerLane)
	require.NoError(t, err)
	require.Len(t, plan.lanes, 1)
	require.Equal(t, "qrl-tests-single", plan.lanes[0].enclaveName)
	require.Equal(t, filepath.Join(reports, "single", "environment.json"), plan.lanes[0].manifestPath)
	require.Contains(t, plan.lanes[0].arguments, "./endtoend/suites/execution/abi")
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
