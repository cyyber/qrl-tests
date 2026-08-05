package main

import (
	"context"
	"fmt"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/urfave/cli/v2"
)

type networkController interface {
	Start(context.Context, devnet.StartOptions) (devnet.Environment, error)
	Stop(context.Context, string) error
}

func networkCommand(network networkController) *cli.Command {
	startFlags := []cli.Flag{
		enclaveNameFlag(),
		backendFlag(),
		&cli.StringFlag{
			Name:    "profile",
			Usage:   "built-in network profile",
			Value:   string(devnet.ProfileSingle),
			EnvVars: []string{"DEVNET_PROFILE"},
		},
		parametersFileFlag(),
		&cli.DurationFlag{
			Name:    "timeout",
			Usage:   "network start budget",
			Value:   devnet.DefaultStartTimeout,
			EnvVars: []string{"DEVNET_START_TIMEOUT"},
		},
	}
	startFlags = append(startFlags, imageFlags()...)
	return &cli.Command{
		Name:  "network",
		Usage: "control a separately managed development network",
		Subcommands: []*cli.Command{
			{
				Name:  "start",
				Usage: "start the development network and wait for readiness",
				Flags: startFlags,
				Action: func(command *cli.Context) error {
					if err := rejectPositional(command); err != nil {
						return err
					}
					parameters, err := readParametersFile(command)
					if err != nil {
						return err
					}
					ctx, cancel := context.WithTimeout(command.Context, command.Duration("timeout"))
					defer cancel()
					if _, err := network.Start(ctx, devnet.StartOptions{
						EnclaveName: command.String("enclave-name"),
						Backend:     devnet.Backend(command.String("backend")),
						Images:      imagesFromFlags(command),
						Parameters:  parameters,
						Profile:     devnet.Profile(command.String("profile")),
					}); err != nil {
						return err
					}
					_, err = fmt.Fprintln(command.App.Writer, "network ready")
					return err
				},
			},
			{
				Name:  "stop",
				Usage: "stop the development network",
				Flags: []cli.Flag{enclaveNameFlag()},
				Action: func(command *cli.Context) error {
					if err := rejectPositional(command); err != nil {
						return err
					}
					if err := network.Stop(command.Context, command.String("enclave-name")); err != nil {
						return err
					}
					_, err := fmt.Fprintln(command.App.Writer, "network stopped")
					return err
				},
			},
		},
	}
}
