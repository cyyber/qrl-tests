package main

import (
	"fmt"

	"github.com/cyyber/qrl-tests/perf/soak"
	"github.com/urfave/cli/v2"
)

func soakCompareCommand() *cli.Command {
	return &cli.Command{
		Name:  "soak-compare",
		Usage: "compare a soak results.json against a previous baseline",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "current",
				Usage:    "this run's results.json",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "baseline",
				Usage:    "previous run's results.json",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "summary",
				Usage: "rewrite this summary.md with week-over-week deltas",
			},
			&cli.StringFlag{
				Name:  "output",
				Usage: "write comparison.json here",
			},
		},
		Action: func(command *cli.Context) error {
			if err := rejectPositional(command); err != nil {
				return err
			}
			comparison, err := soak.WriteComparison(
				command.String("current"),
				command.String("baseline"),
				command.String("summary"),
				command.String("output"),
			)
			if err != nil {
				return err
			}
			if comparison.Comparable {
				fmt.Fprintf(command.App.Writer, "compared %d metrics against the previous soak\n", len(comparison.Deltas))
				return nil
			}
			fmt.Fprintf(command.App.Writer, "skipped baseline comparison: %s\n", comparison.Reason)
			return nil
		},
	}
}
