package main

import (
	"fmt"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/perf/soak"
	"github.com/urfave/cli/v2"
)

func soakCommand() *cli.Command {
	return &cli.Command{
		Name:  "soak",
		Usage: "provision a soak network, sample it, and write a verdict",
		Flags: soakFlags(),
		Action: func(command *cli.Context) error {
			if err := rejectPositional(command); err != nil {
				return err
			}
			configuration, err := soakConfig(command)
			if err != nil {
				return err
			}
			return soak.New(configuration, command.App.Writer, command.App.ErrWriter).Run(command.Context)
		},
	}
}

func soakFlags() []cli.Flag {
	flags := []cli.Flag{
		enclaveNameFlag(),
		parametersFileFlag(),
		&cli.StringFlag{
			Name:    "report-dir",
			Usage:   "soak report directory",
			Value:   soak.DefaultReportDir,
			EnvVars: []string{"SOAK_REPORT_DIR"},
		},
		backendFlag(),
		endpointModeFlag(),
		loadPercentFlag(),
		&cli.DurationFlag{
			Name:    "start-timeout",
			Usage:   "network start budget",
			Value:   soak.DefaultStartTimeout,
			EnvVars: []string{"DEVNET_START_TIMEOUT"},
		},
		&cli.DurationFlag{
			Name:    "duration",
			Usage:   "steady-state window",
			Value:   soak.DefaultDuration,
			EnvVars: []string{"SOAK_DURATION"},
		},
		&cli.DurationFlag{
			Name:    "interval",
			Usage:   "sampling interval",
			Value:   soak.DefaultInterval,
			EnvVars: []string{"SOAK_INTERVAL"},
		},
		&cli.BoolFlag{
			Name:    "enforce",
			Usage:   "fail when a gate is breached (off while calibrating)",
			EnvVars: []string{"SOAK_ENFORCE"},
		},
		&cli.BoolFlag{
			Name:    "keep-network",
			Usage:   "leave the provisioned network running after the soak finishes",
			EnvVars: []string{"KEEP_NETWORK"},
		},
		&cli.BoolFlag{
			Name:    "existing",
			Usage:   "sample an already-running network instead of provisioning one",
			EnvVars: []string{"SOAK_EXISTING"},
		},
		&cli.StringFlag{
			Name:    "thresholds",
			Usage:   "override path for soak gate thresholds",
			EnvVars: []string{"SOAK_THRESHOLDS"},
		},
	}
	return append(flags, imageFlags()...)
}

func soakConfig(command *cli.Context) (soak.Config, error) {
	if command.Bool("existing") && command.Path("params-file") != "" {
		return soak.Config{}, fmt.Errorf("custom parameters cannot be used with --existing")
	}

	duration := command.Duration("duration")
	if duration <= 0 {
		return soak.Config{}, fmt.Errorf("duration must be positive")
	}

	backend, err := devnet.ParseBackend(command.String("backend"))
	if err != nil {
		return soak.Config{}, err
	}
	endpointMode, err := devnet.ParseEndpointMode(command.String("endpoint-mode"))
	if err != nil {
		return soak.Config{}, err
	}
	parameters, err := readParametersFile(command)
	if err != nil {
		return soak.Config{}, err
	}

	return soak.Config{
		EnclaveName:    command.String("enclave-name"),
		ReportDir:      command.String("report-dir"),
		Backend:        backend,
		EndpointMode:   endpointMode,
		LoadPercent:    command.Int("load-percent"),
		StartTimeout:   command.Duration("start-timeout"),
		Duration:       duration,
		Interval:       command.Duration("interval"),
		Enforce:        command.Bool("enforce"),
		KeepNetwork:    command.Bool("keep-network"),
		Existing:       command.Bool("existing"),
		ThresholdsPath: command.String("thresholds"),
		Parameters:     parameters,
		Images:         imagesFromFlags(command),
	}, nil
}
