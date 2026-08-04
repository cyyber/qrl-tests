// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

// Command qrltest controls development networks and executes E2E lanes.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/urfave/cli/v2"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := newApp(devnet.NewManager()).RunContext(ctx, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "qrltest:", err)
		os.Exit(1)
	}
}

func newApp(networks controller) *cli.App {
	commands := []*cli.Command{networkCommand(networks)}
	commands = append(commands, runnerCommands()...)
	return &cli.App{
		Name:            "qrltest",
		Usage:           "control QRL test networks and execute E2E lanes",
		HideHelpCommand: true,
		Action:          rootAction,
		Commands:        commands,
	}
}

func rootAction(command *cli.Context) error {
	if command.Args().Present() {
		return fmt.Errorf("unknown command %q", command.Args().First())
	}
	return cli.ShowAppHelp(command)
}

func rejectPositional(command *cli.Context) error {
	if command.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", command.Args().Slice())
	}
	return nil
}
