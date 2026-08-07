package runner

import (
	"cmp"
	"fmt"
	"path/filepath"

	"github.com/cyyber/qrl-tests/e2e/internal/lanes"
)

type laneRun struct {
	lane         lanes.Lane
	enclaveName  string
	reportDir    string
	manifestPath string
	arguments    []string
	provision    bool
	testsDir     string
}

func planLanes(configuration Config, selected []lanes.Lane, mode runMode) ([]laneRun, error) {
	testsDir, err := filepath.Abs(cmp.Or(configuration.TestsDir, "."))
	if err != nil {
		return nil, fmt.Errorf("resolve test source directory: %w", err)
	}

	reportRoot, err := filepath.Abs(cmp.Or(configuration.ReportDir, DefaultReportDir))
	if err != nil {
		return nil, fmt.Errorf("resolve report directory: %w", err)
	}

	planned := make([]laneRun, len(selected))
	for index, lane := range selected {
		enclaveName := configuration.BaseName
		if mode.suffixesEnclave() {
			enclaveName += "-" + lane.Name
		}
		reportDir := filepath.Join(reportRoot, lane.Name)
		planned[index] = laneRun{
			lane:         lane,
			enclaveName:  enclaveName,
			reportDir:    reportDir,
			manifestPath: filepath.Join(reportDir, "manifest.json"),
			arguments:    ginkgoArguments(lane, reportDir),
			provision:    mode.provisions(),
			testsDir:     testsDir,
		}
	}
	return planned, nil
}

func ginkgoArguments(lane lanes.Lane, reportDir string) []string {
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
