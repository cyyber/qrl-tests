package results

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/ginkgo/v2/types"
	"github.com/stretchr/testify/require"
)

const testLaneName = "execution-abi"

func specReport(state types.SpecState, text string) types.SpecReport {
	report := types.SpecReport{
		ContainerHierarchyTexts: []string{"ABI"},
		LeafNodeText:            text,
		LeafNodeType:            types.NodeTypeIt,
		State:                   state,
	}
	if state.Is(types.SpecStateFailureStates) {
		report.Failure = types.Failure{
			Message:  "expected 1, got 2",
			Location: types.CodeLocation{FileName: "calls_test.go", LineNumber: 12},
		}
	}
	return report
}

func suiteReport(name string, specs ...types.SpecReport) types.Report {
	return types.Report{
		SuiteDescription: name,
		SuiteSucceeded:   true,
		SpecReports:      specs,
	}
}

func executionOutcome(name string, executionErr error, reports ...types.Report) Outcome {
	return Outcome{
		Name:         name,
		Err:          executionErr,
		ExecutionErr: executionErr,
		reports:      reports,
	}
}

func summarizeTestLane(executionErr error, reports ...types.Report) LaneSummary {
	return summarizeOutcome(executionOutcome(testLaneName, executionErr, reports...))
}

func writeReportFile(t *testing.T, laneDir string, specs ...types.SpecReport) {
	t.Helper()
	payload, err := json.Marshal([]types.Report{suiteReport(filepath.Base(laneDir), specs...)})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(laneDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(laneDir, ReportFileName), payload, 0o600))
}

func outcomeFromReportDir(name, reportDir string, err error) Outcome {
	outcome := Outcome{Name: name, Err: err, ExecutionErr: err}
	outcome.CaptureReports(reportDir)
	return outcome
}

func TestCaptureReportsMissingFile(t *testing.T) {
	root := t.TempDir()
	outcome := outcomeFromReportDir(
		testLaneName,
		filepath.Join(root, "lanes", testLaneName),
		errors.New("exit status 1"),
	)
	lane := summarizeOutcome(outcome)

	require.Equal(t, ClassInfrastructure, lane.Class,
		"without a report nothing distinguishes a product failure from a broken harness")
	require.Contains(t, lane.Error, "exit status 1")
	require.Contains(t, lane.Error, "report.json")
	require.Contains(t, lane.Error, "no such file or directory")
}

func TestCaptureReportsRejectsCorruptFile(t *testing.T) {
	laneDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(laneDir, ReportFileName), []byte("{"), 0o600))

	lane := summarizeOutcome(outcomeFromReportDir(testLaneName, laneDir, nil))
	require.Equal(t, ClassInfrastructure, lane.Class)
	require.Contains(t, lane.Error, "decode ")
	require.Contains(t, lane.Error, "report.json")
}

func TestCaptureReportsUsesCapturedSnapshot(t *testing.T) {
	laneDir := t.TempDir()
	writeReportFile(t, laneDir, specReport(types.SpecStatePassed, "encodes calls"))
	outcome := outcomeFromReportDir(testLaneName, laneDir, nil)

	writeReportFile(t, laneDir, specReport(types.SpecStateFailed, "encodes calls"))
	lane := summarizeOutcome(outcome)
	require.Equal(t, ClassPassed, lane.Class)
}

func TestSummarizeWritesPassedLane(t *testing.T) {
	root := t.TempDir()
	laneDir := filepath.Join(root, "lanes", testLaneName)
	writeReportFile(t, laneDir,
		specReport(types.SpecStatePassed, "encodes calls"),
		specReport(types.SpecStatePassed, "decodes events"),
	)

	outcome := outcomeFromReportDir(testLaneName, laneDir, nil)
	require.True(t, outcome.Passed())

	summary, err := Summarize(root, []Outcome{outcome})
	require.NoError(t, err)
	require.Equal(t, []suiteSummary{{
		Name:   testLaneName,
		Class:  ClassPassed,
		Counts: Counts{Specs: 2, Passed: 2},
	}}, summary.Lanes[0].suites)

	payload, err := os.ReadFile(filepath.Join(root, SummaryFileName))
	require.NoError(t, err)
	require.NotContains(t, string(payload), `"suites"`)

	var written Summary
	require.NoError(t, json.Unmarshal(payload, &written))
	require.Equal(t, Summary{
		Result: "passed",
		Totals: Counts{Specs: 2, Passed: 2},
		Lanes: []LaneSummary{{
			Name:   testLaneName,
			Class:  ClassPassed,
			Counts: Counts{Specs: 2, Passed: 2},
		}},
	}, written)
}

func TestSummarizeReturnsVerdictWhenPersistenceFails(t *testing.T) {
	reportRoot := filepath.Join(t.TempDir(), "report")
	require.NoError(t, os.WriteFile(reportRoot, []byte("occupied"), 0o600))

	summary, err := Summarize(reportRoot, []Outcome{
		executionOutcome(testLaneName, errors.New("exit status 1"),
			suiteReport(testLaneName, specReport(types.SpecStateFailed, "decodes events")),
		),
	})

	require.ErrorContains(t, err, "create report directory")
	require.Equal(t, "failed", summary.Result)
	require.Equal(t, ClassAssertion, summary.Lanes[0].Class)
}

func TestSummarizeClassifiesAssertionFailures(t *testing.T) {
	lane := summarizeTestLane(errors.New("exit status 1"),
		suiteReport(testLaneName,
			specReport(types.SpecStatePassed, "encodes calls"),
			specReport(types.SpecStateFailed, "decodes events"),
		),
	)

	require.Equal(t, ClassAssertion, lane.Class)
	require.Equal(t, Counts{Specs: 2, Passed: 1, Failed: 1}, lane.Counts)
	suite := lane.suites[0]
	require.Len(t, suite.Failures, 1)
	require.Equal(t, "ABI decodes events", suite.Failures[0].Spec)
	require.Equal(t, "expected 1, got 2", suite.Failures[0].Message)
	require.Equal(t, "calls_test.go:12", suite.Failures[0].Location)
}

func TestSummarizeClassifiesTimeouts(t *testing.T) {
	lane := summarizeTestLane(errors.New("exit status 1"),
		suiteReport(testLaneName,
			specReport(types.SpecStateFailed, "decodes events"),
			specReport(types.SpecStateTimedout, "streams blocks"),
		),
	)

	require.Equal(t, ClassTimeout, lane.Class,
		"a lane that hit its deadline is a timeout even when other specs failed")
}

func TestSummarizeClassifiesSuiteTimeouts(t *testing.T) {
	report := types.Report{
		SuiteDescription:           testLaneName,
		SpecialSuiteFailureReasons: []string{"Suite did not run because the timeout elapsed"},
	}
	lane := summarizeTestLane(errors.New("exit status 1"), report)

	require.Equal(t, ClassTimeout, lane.Class)
	require.Equal(t, report.SpecialSuiteFailureReasons, lane.suites[0].SuiteFailures)
	require.Contains(t, lane.Error, "exit status 1")
}

func TestSummarizeClassifiesExecutionContext(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "canceled", err: context.Canceled, want: ClassCanceled},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: ClassTimeout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lane := summarizeTestLane(test.err,
				suiteReport(testLaneName, specReport(types.SpecStateInterrupted, "streams blocks")),
			)

			require.Equal(t, test.want, lane.Class)
		})
	}
}

func TestSummarizeHonorsFailedSuiteReport(t *testing.T) {
	lane := summarizeTestLane(nil, types.Report{
		SuiteDescription: testLaneName,
		SuiteSucceeded:   false,
		SpecReports:      []types.SpecReport{specReport(types.SpecStatePassed, "encodes calls")},
	})

	require.Equal(t, ClassInfrastructure, lane.Class)
}

func TestSummarizeClassifiesUnexpectedSkips(t *testing.T) {
	tests := []struct {
		name  string
		state types.SpecState
		err   error
	}{
		{name: "skipped spec", state: types.SpecStateSkipped},
		{name: "pending spec with nonzero exit", state: types.SpecStatePending, err: errors.New("exit status 1")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lane := summarizeTestLane(test.err,
				suiteReport(testLaneName,
					specReport(types.SpecStatePassed, "encodes calls"),
					specReport(test.state, "decodes events"),
				),
			)

			require.Equal(t, ClassSkipped, lane.Class)
			require.Equal(t, []string{"ABI decodes events"}, lane.UnexpectedSkips)
		})
	}
}

func TestSummarizeHonorsBootstrapFailure(t *testing.T) {
	lane := summarizeOutcome(Outcome{
		Name:            testLaneName,
		Err:             errors.New("network bootstrap failed: start network: no capacity"),
		BootstrapFailed: true,
	})

	require.Equal(t, ClassBootstrap, lane.Class)
	require.NotEmpty(t, lane.Error)
}

func TestSummarizeCountsSetupFailures(t *testing.T) {
	setup := types.SpecReport{
		LeafNodeType: types.NodeTypeSynchronizedBeforeSuite,
		State:        types.SpecStateFailed,
		Failure:      types.Failure{Message: "wallet not funded"},
	}
	lane := summarizeTestLane(errors.New("exit status 1"),
		suiteReport(testLaneName, setup, specReport(types.SpecStateSkipped, "decodes events")),
	)

	require.Equal(t, ClassAssertion, lane.Class)
	require.Len(t, lane.suites[0].Failures, 1)
	require.Equal(t, Counts{Specs: 1, Skipped: 1}, lane.Counts,
		"setup nodes are not specs; the skipped spec still counts")
}

func TestSummarizeTreatsLifecycleFailureAsInfrastructure(t *testing.T) {
	lane := summarizeOutcome(Outcome{
		Name: testLaneName,
		Err:  errors.New("stop network: context deadline exceeded"),
		reports: []types.Report{
			suiteReport(testLaneName, specReport(types.SpecStatePassed, "encodes calls")),
		},
	})

	require.Equal(t, ClassInfrastructure, lane.Class)
	require.Equal(t, "stop network: context deadline exceeded", lane.Error)
}

func TestSummarizeAggregatesLanes(t *testing.T) {
	summary, err := Summarize(t.TempDir(), []Outcome{
		executionOutcome(testLaneName, nil,
			suiteReport(testLaneName, specReport(types.SpecStatePassed, "encodes calls")),
		),
		executionOutcome("consensus", errors.New("exit status 1"),
			suiteReport("consensus", specReport(types.SpecStateFailed, "finalizes")),
		),
	})

	require.NoError(t, err)
	require.Equal(t, "failed", summary.Result)
	require.Equal(t, Counts{Specs: 2, Passed: 1, Failed: 1}, summary.Totals)
}

func TestSummarizeReportsSuitesWithinLane(t *testing.T) {
	lane := summarizeOutcome(executionOutcome("execution", errors.New("exit status 1"),
		suiteReport("ABI E2E suite", specReport(types.SpecStatePassed, "encodes calls")),
		suiteReport("API E2E suite", specReport(types.SpecStateFailed, "decodes events")),
	))

	require.Equal(t, ClassAssertion, lane.Class)
	require.Equal(t, Counts{Specs: 2, Passed: 1, Failed: 1}, lane.Counts)
	require.Len(t, lane.suites, 2)
	require.Equal(t, "ABI E2E suite", lane.suites[0].Name)
	require.Equal(t, ClassPassed, lane.suites[0].Class)
	require.Equal(t, "API E2E suite", lane.suites[1].Name)
	require.Equal(t, ClassAssertion, lane.suites[1].Class)
}

func TestSummarizeRejectsEmptySuiteWithinLane(t *testing.T) {
	lane := summarizeOutcome(executionOutcome("execution", nil,
		types.Report{SuiteDescription: "Empty E2E suite", SuiteSucceeded: true},
		suiteReport("ABI E2E suite", specReport(types.SpecStatePassed, "encodes calls")),
	))

	require.Equal(t, ClassInfrastructure, lane.Class)
	require.Equal(t, ClassInfrastructure, lane.suites[0].Class)
	require.Equal(t, ClassPassed, lane.suites[1].Class)
}

func TestSummarizeEmptyReportIsInfrastructure(t *testing.T) {
	lane := summarizeTestLane(nil, suiteReport(testLaneName))
	require.Equal(t, ClassInfrastructure, lane.Class)
}

func TestVerdictError(t *testing.T) {
	summary := Summary{Lanes: []LaneSummary{{Name: "abi", Class: ClassPassed}}}
	require.NoError(t, summary.VerdictError())

	summary.Lanes = append(summary.Lanes, LaneSummary{
		Name:  "sync",
		Class: ClassInfrastructure,
		Error: "exit status 1",
	})
	require.EqualError(t, summary.VerdictError(), "lanes did not pass: sync (infrastructure): exit status 1")
}
