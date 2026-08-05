package runner

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cyyber/qrl-tests/endtoend/internal/lanes"
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

type runPlan struct {
	reportRoot string
	testsDir   string
	lanes      []laneRun
}

func newRunPlan(configuration Config, selected []lanes.Lane, mode runMode) (runPlan, error) {
	testsDir := configuration.TestsDir
	if testsDir == "" {
		var err error
		testsDir, err = os.Getwd()
		if err != nil {
			return runPlan{}, fmt.Errorf("resolve test source directory: %w", err)
		}
	}
	testsDir, err := filepath.Abs(testsDir)
	if err != nil {
		return runPlan{}, fmt.Errorf("resolve test source directory: %w", err)
	}
	reportRoot, err := filepath.Abs(configuration.ReportDir)
	if err != nil {
		return runPlan{}, fmt.Errorf("resolve report directory: %w", err)
	}
	plan := runPlan{
		reportRoot: reportRoot,
		testsDir:   testsDir,
		lanes:      make([]laneRun, len(selected)),
	}
	for index, lane := range selected {
		enclaveName := configuration.BaseName
		if mode.suffixesEnclave() {
			enclaveName += "-" + lane.Name
		}
		reportDir := filepath.Join(reportRoot, lane.Name)
		plan.lanes[index] = laneRun{
			lane:         lane,
			enclaveName:  enclaveName,
			reportDir:    reportDir,
			manifestPath: filepath.Join(reportDir, "environment.json"),
			arguments:    ginkgoArguments(lane, reportDir),
			provision:    mode.provisions(),
			testsDir:     testsDir,
		}
	}
	return plan, nil
}
