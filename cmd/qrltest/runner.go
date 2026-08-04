// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

package main

import (
	"fmt"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/endtoend/runner"
	"github.com/urfave/cli/v2"
)

type runnerAction func(*runner.Runner, *cli.Context, string) error

func runnerCommands() []*cli.Command {
	return []*cli.Command{
		{
			Name:  "list",
			Usage: "list registered E2E lanes",
			Action: func(command *cli.Context) error {
				if err := rejectPositional(command); err != nil {
					return err
				}
				return runner.New(runner.Config{}, command.App.Writer, command.App.ErrWriter).List()
			},
		},
		laneCommand("test", "run a lane against an existing network", true, func(tests *runner.Runner, command *cli.Context, lane string) error {
			return tests.Test(command.Context, lane)
		}),
		laneCommand("run", "provision, execute, and stop one E2E lane", true, func(tests *runner.Runner, command *cli.Context, lane string) error {
			return tests.Run(command.Context, lane)
		}),
		laneCommand("run-all", "provision and execute all supported E2E lanes", false, func(tests *runner.Runner, command *cli.Context, _ string) error {
			return tests.RunAll(command.Context)
		}),
	}
}

func laneCommand(name, usage string, requiresLane bool, action runnerAction) *cli.Command {
	return &cli.Command{
		Name:  name,
		Usage: usage,
		Flags: runnerFlags(),
		Action: func(command *cli.Context) error {
			lane := ""
			if requiresLane {
				if command.NArg() != 1 {
					return fmt.Errorf("%s requires one lane name", name)
				}
				lane = command.Args().First()
			} else if err := rejectPositional(command); err != nil {
				return err
			}
			configuration, err := runnerConfig(command)
			if err != nil {
				return err
			}
			tests := runner.New(configuration, command.App.Writer, command.App.ErrWriter)
			return action(tests, command, lane)
		},
	}
}

func runnerFlags() []cli.Flag {
	flags := []cli.Flag{
		&cli.StringFlag{Name: "source-dir", Usage: "go-qrl source checkout", EnvVars: []string{"GO_QRL_SOURCE_DIR"}},
		&cli.StringFlag{Name: "tests-dir", Usage: "qrl-tests source checkout", Value: ".", EnvVars: []string{"QRL_TESTS_SOURCE_DIR"}},
		enclaveNameFlag(),
		parametersFileFlag(),
		&cli.StringSliceFlag{Name: "suite", Usage: "run only the named suite within the selected lane"},
		&cli.StringFlag{Name: "report-dir", Usage: "E2E report directory", Value: runner.DefaultReportDir, EnvVars: []string{"E2E_REPORT_DIR"}},
		&cli.IntFlag{Name: "max-parallel", Usage: "maximum concurrently provisioned E2E lanes", Value: 1, EnvVars: []string{"E2E_MAX_PARALLEL"}},
		backendFlag(),
		&cli.DurationFlag{Name: "start-timeout", Usage: "network start budget", Value: devnet.DefaultStartTimeout, EnvVars: []string{"DEVNET_START_TIMEOUT"}},
	}
	return append(flags, imageFlags()...)
}

func runnerConfig(command *cli.Context) (runner.Config, error) {
	backend, err := devnet.ParseBackend(command.String("backend"))
	if err != nil {
		return runner.Config{}, err
	}
	if command.Int("max-parallel") < 1 {
		return runner.Config{}, fmt.Errorf("max-parallel must be at least 1")
	}
	parameters, err := parametersFrom(command)
	if err != nil {
		return runner.Config{}, err
	}
	return runner.Config{
		SourceDir:    command.String("source-dir"),
		TestsDir:     command.String("tests-dir"),
		BaseName:     command.String("enclave-name"),
		ReportDir:    command.String("report-dir"),
		Backend:      backend,
		Parameters:   parameters,
		Suites:       command.StringSlice("suite"),
		StartTimeout: command.Duration("start-timeout"),
		MaxParallel:  command.Int("max-parallel"),
		Images:       imagesFrom(command),
	}, nil
}
