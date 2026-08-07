package runner

import (
	"cmp"
	"fmt"
	"math/rand/v2"
	"path/filepath"

	"github.com/cyyber/qrl-tests/e2e/internal/lanes"
)

type laneRun struct {
	lane         lanes.Lane
	enclaveName  string
	reportDir    string
	manifestPath string
	seed         int64
	arguments    []string
	provision    bool
	testsDir     string
}

func planLanes(configuration Config, selected []lanes.Lane, mode runMode) ([]laneRun, string, error) {
	testsDir, err := filepath.Abs(cmp.Or(configuration.TestsDir, "."))
	if err != nil {
		return nil, "", fmt.Errorf("resolve test source directory: %w", err)
	}

	reportRoot, err := filepath.Abs(cmp.Or(configuration.ReportDir, DefaultReportDir))
	if err != nil {
		return nil, "", fmt.Errorf("resolve report directory: %w", err)
	}

	planned := make([]laneRun, len(selected))
	for index, lane := range selected {
		enclaveName := configuration.BaseName
		if mode.suffixesEnclave() {
			enclaveName += "-" + lane.Name
		}
		reportDir := filepath.Join(reportRoot, "lanes", lane.Name)
		// The seed randomizes ginkgo's spec order; recording it in the run
		// manifest keeps every ordering reproducible.
		seed := 1 + rand.Int64N(1<<31-1)
		planned[index] = laneRun{
			lane:         lane,
			enclaveName:  enclaveName,
			reportDir:    reportDir,
			manifestPath: filepath.Join(reportDir, "manifest.json"),
			seed:         seed,
			arguments:    ginkgoArguments(lane, reportDir, seed),
			provision:    mode.provisions(),
			testsDir:     testsDir,
		}
	}
	return planned, reportRoot, nil
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
