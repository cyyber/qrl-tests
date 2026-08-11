package main

import (
	"fmt"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/runner"
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

				testRunner := runner.New(runner.Config{}, command.App.Writer, command.App.ErrWriter)
				return testRunner.List()
			},
		},
		laneCommand(
			"test",
			"run a lane against an existing network",
			func(testRunner *runner.Runner, command *cli.Context, lane string) error {
				return testRunner.Test(command.Context, lane)
			},
		),
		laneCommand(
			"run",
			"provision, execute, and stop one E2E lane",
			func(testRunner *runner.Runner, command *cli.Context, lane string) error {
				return testRunner.Run(command.Context, lane)
			},
		),
		{
			Name:  "run-all",
			Usage: "provision and execute all supported E2E lanes",
			Flags: runnerFlags(),
			Action: func(command *cli.Context) error {
				if err := rejectPositional(command); err != nil {
					return err
				}

				configuration, err := runnerConfig(command)
				if err != nil {
					return err
				}

				testRunner := runner.New(configuration, command.App.Writer, command.App.ErrWriter)
				return testRunner.RunAll(command.Context)
			},
		},
	}
}

func laneCommand(name, usage string, action runnerAction) *cli.Command {
	return &cli.Command{
		Name:  name,
		Usage: usage,
		Flags: runnerFlags(),
		Action: func(command *cli.Context) error {
			if command.NArg() != 1 {
				return fmt.Errorf("%s requires one lane name", name)
			}

			configuration, err := runnerConfig(command)
			if err != nil {
				return err
			}

			testRunner := runner.New(configuration, command.App.Writer, command.App.ErrWriter)
			return action(testRunner, command, command.Args().First())
		},
	}
}

func runnerFlags() []cli.Flag {
	flags := []cli.Flag{
		&cli.StringFlag{
			Name:    "tests-dir",
			Usage:   "qrl-tests source checkout",
			Value:   ".",
			EnvVars: []string{"QRL_TESTS_SOURCE_DIR"},
		},
		enclaveNameFlag(),
		parametersFileFlag(),
		&cli.StringSliceFlag{
			Name:  "suite",
			Usage: "run only the named suite within the selected lane",
		},
		&cli.StringFlag{
			Name:    "report-dir",
			Usage:   "E2E report directory",
			Value:   runner.DefaultReportDir,
			EnvVars: []string{"E2E_REPORT_DIR"},
		},
		&cli.IntFlag{
			Name:    "max-parallel",
			Usage:   "maximum concurrently provisioned E2E lanes",
			Value:   1,
			EnvVars: []string{"E2E_MAX_PARALLEL"},
		},
		backendFlag(),
		&cli.DurationFlag{
			Name:    "start-timeout",
			Usage:   "network start budget",
			Value:   devnet.DefaultStartTimeout,
			EnvVars: []string{"DEVNET_START_TIMEOUT"},
		},
		&cli.StringFlag{
			Name:    "diagnostics",
			Usage:   "network diagnostics collection: on-failure, always, or never",
			Value:   string(runner.DiagnosticsOnFailure),
			EnvVars: []string{"E2E_DIAGNOSTICS"},
		},
	}

	return append(flags, imageFlags()...)
}

func runnerConfig(command *cli.Context) (runner.Config, error) {
	backend, err := devnet.ParseBackend(command.String("backend"))
	if err != nil {
		return runner.Config{}, err
	}

	maxParallel := command.Int("max-parallel")
	if maxParallel < 1 {
		return runner.Config{}, fmt.Errorf("max-parallel must be at least 1")
	}

	diagnostics, err := runner.ParseDiagnosticsMode(command.String("diagnostics"))
	if err != nil {
		return runner.Config{}, err
	}

	parameters, err := readParametersFile(command)
	if err != nil {
		return runner.Config{}, err
	}

	return runner.Config{
		TestsDir:     command.String("tests-dir"),
		BaseName:     command.String("enclave-name"),
		ReportDir:    command.String("report-dir"),
		Backend:      backend,
		Parameters:   parameters,
		Suites:       command.StringSlice("suite"),
		StartTimeout: command.Duration("start-timeout"),
		MaxParallel:  maxParallel,
		Images:       imagesFromFlags(command),
		Diagnostics:  diagnostics,
	}, nil
}
