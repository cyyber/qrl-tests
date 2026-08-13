package runner

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"

	"github.com/cyyber/qrl-tests/e2e/internal/lanes"
)

type runPlan struct {
	testsDir   string
	reportRoot string
	mode       runMode
	lanes      []laneRun
}

type laneRun struct {
	definition  lanes.Lane
	enclaveName string
	reportDir   string
	seed        int64
}

func planLanes(configuration Config, selected []lanes.Lane, mode runMode) (runPlan, error) {
	testsDir, err := filepath.Abs(configuration.TestsDir)
	if err != nil {
		return runPlan{}, fmt.Errorf("resolve test source directory: %w", err)
	}

	reportRoot, err := filepath.Abs(configuration.ReportDir)
	if err != nil {
		return runPlan{}, fmt.Errorf("resolve report directory: %w", err)
	}

	planned := make([]laneRun, len(selected))
	for index, lane := range selected {
		enclaveName := configuration.BaseName
		if mode.suffixesEnclave() {
			enclaveName += "-" + lane.Name
		}
		reportDir := filepath.Join(reportRoot, "lanes", lane.Name)
		// A stale report from an earlier run must never feed this run's verdict.
		if err := os.RemoveAll(reportDir); err != nil {
			return runPlan{}, fmt.Errorf("clear %s: %w", reportDir, err)
		}
		// The seed randomizes ginkgo's spec order; recording it in the run
		// manifest keeps every ordering reproducible, and a configured seed
		// replays a recorded one exactly.
		seed := configuration.Seed
		if seed == 0 {
			seed = 1 + rand.Int64N(1<<31-1)
		}
		planned[index] = laneRun{
			definition:  lane,
			enclaveName: enclaveName,
			reportDir:   reportDir,
			seed:        seed,
		}
	}
	return runPlan{testsDir: testsDir, reportRoot: reportRoot, mode: mode, lanes: planned}, nil
}

func (lane laneRun) manifestPath() string {
	return filepath.Join(lane.reportDir, "manifest.json")
}

func (lane laneRun) ginkgoArguments() []string {
	return ginkgoArguments(lane.definition, lane.reportDir, lane.seed)
}

func ginkgoArguments(lane lanes.Lane, reportDir string, seed int64) []string {
	arguments := []string{
		"tool", "ginkgo",
		"--tags=e2e",
		// --procs=1: suites share one funded wallet, so specs must stay in a
		// single process to keep its nonce sequence serial.
		"--procs=1",
		"--keep-going",
		"--require-suite",
		"--fail-on-empty",
		"--fail-on-pending",
		fmt.Sprintf("--seed=%d", seed),
		"--timeout=" + lane.Timeout.String(),
		"--output-dir=" + reportDir,
		"--junit-report=junit.xml",
		"--json-report=report.json",
	}
	arguments = append(arguments, lane.Packages()...)
	// Every suite package defines exactly one Go test entrypoint named TestE2E;
	// --fail-on-empty turns a misnamed entrypoint into a failed lane.
	return append(arguments, "--", "-test.run=^TestE2E$")
}
