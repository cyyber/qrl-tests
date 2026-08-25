package results

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cyyber/qrl-tests/internal/testutil"
	"github.com/onsi/ginkgo/v2/types"
	"github.com/stretchr/testify/require"
)

const testLaneName = "execution"

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
	testutil.WriteJSON(
		t,
		filepath.Join(laneDir, ReportFileName),
		[]types.Report{suiteReport(filepath.Base(laneDir), specs...)},
	)
}

func outcomeFromReportDir(reportDir string, executionErr error) Outcome {
	outcome := Outcome{Name: testLaneName, Err: executionErr, ExecutionErr: executionErr}
	outcome.CaptureReports(reportDir)
	return outcome
}

func TestCaptureReportsMissingFile(t *testing.T) {
	root := t.TempDir()
	outcome := outcomeFromReportDir(
		filepath.Join(root, "lanes", testLaneName),
		errors.New("exit status 1"),
	)
	summary := summarizeOutcome(outcome)

	require.Equal(t, VerdictInfrastructure, summary.Verdict,
		"without a report nothing distinguishes a product failure from a broken harness")
	require.Contains(t, summary.Error, "exit status 1")
	require.Contains(t, summary.Error, "report.json")
}

func TestCaptureReportsRejectsCorruptFile(t *testing.T) {
	laneDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(laneDir, ReportFileName), []byte("{"), 0o600))

	summary := summarizeOutcome(outcomeFromReportDir(laneDir, nil))
	require.Equal(t, VerdictInfrastructure, summary.Verdict)
	require.Contains(t, summary.Error, "decode ")
	require.Contains(t, summary.Error, "report.json")
}

func TestCaptureReportsUsesCapturedSnapshot(t *testing.T) {
	laneDir := t.TempDir()
	writeReportFile(t, laneDir, specReport(types.SpecStatePassed, "encodes calls"))
	outcome := outcomeFromReportDir(laneDir, nil)

	writeReportFile(t, laneDir, specReport(types.SpecStateFailed, "encodes calls"))
	summary := summarizeOutcome(outcome)
	require.Equal(t, VerdictPassed, summary.Verdict)
}

func TestSummarizeWritesPassedLane(t *testing.T) {
	root := t.TempDir()
	laneDir := filepath.Join(root, "lanes", testLaneName)
	writeReportFile(t, laneDir,
		specReport(types.SpecStatePassed, "encodes calls"),
		specReport(types.SpecStatePassed, "decodes events"),
	)

	outcome := outcomeFromReportDir(laneDir, nil)
	require.True(t, outcome.Passed())

	summary, err := Summarize(root, []Outcome{outcome})
	require.NoError(t, err)
	require.Equal(t, []suiteSummary{{
		Name:    testLaneName,
		Verdict: VerdictPassed,
		Counts:  Counts{Specs: 2, Passed: 2},
	}}, summary.Lanes[0].suites)

	summaryPath := filepath.Join(root, SummaryFileName)
	payload, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	require.NotContains(t, string(payload), `"suites"`)

	written := testutil.ReadJSON[Summary](t, summaryPath)
	require.Equal(t, Summary{
		Result: "passed",
		Totals: Counts{Specs: 2, Passed: 2},
		Lanes: []LaneSummary{{
			Name:    testLaneName,
			Verdict: VerdictPassed,
			Counts:  Counts{Specs: 2, Passed: 2},
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

	require.ErrorContains(t, err, "create summary directory")
	require.Equal(t, "failed", summary.Result)
	require.Equal(t, VerdictAssertion, summary.Lanes[0].Verdict)
}

func TestSummarizeClassifiesAssertionFailures(t *testing.T) {
	summary := summarizeTestLane(errors.New("exit status 1"),
		suiteReport(testLaneName,
			specReport(types.SpecStatePassed, "encodes calls"),
			specReport(types.SpecStateFailed, "decodes events"),
		),
	)

	require.Equal(t, VerdictAssertion, summary.Verdict)
	require.Equal(t, Counts{Specs: 2, Passed: 1, Failed: 1}, summary.Counts)
	suite := summary.suites[0]
	require.Len(t, suite.Failures, 1)
	require.Equal(t, "ABI decodes events", suite.Failures[0].Spec)
	require.Equal(t, "expected 1, got 2", suite.Failures[0].Message)
	require.Equal(t, "calls_test.go:12", suite.Failures[0].Location)
}

func TestSummarizeClassifiesTimeouts(t *testing.T) {
	summary := summarizeTestLane(errors.New("exit status 1"),
		suiteReport(testLaneName,
			specReport(types.SpecStateFailed, "decodes events"),
			specReport(types.SpecStateTimedout, "streams blocks"),
		),
	)

	require.Equal(t, VerdictTimeout, summary.Verdict,
		"a lane that hit its deadline is a timeout even when other specs failed")
}

func TestSummarizeClassifiesSuiteTimeouts(t *testing.T) {
	report := types.Report{
		SuiteDescription:           testLaneName,
		SpecialSuiteFailureReasons: []string{"Suite did not run because the timeout elapsed"},
	}
	summary := summarizeTestLane(errors.New("exit status 1"), report)

	require.Equal(t, VerdictTimeout, summary.Verdict)
	require.Equal(t, report.SpecialSuiteFailureReasons, summary.suites[0].SuiteFailures)
	require.Contains(t, summary.Error, "exit status 1")
}

func TestSummarizeClassifiesExecutionContext(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "canceled", err: context.Canceled, want: VerdictCanceled},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: VerdictTimeout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			summary := summarizeTestLane(test.err,
				suiteReport(testLaneName, specReport(types.SpecStateInterrupted, "streams blocks")),
			)

			require.Equal(t, test.want, summary.Verdict)
		})
	}
}

func TestSummarizeHonorsFailedSuiteReport(t *testing.T) {
	summary := summarizeTestLane(nil, types.Report{
		SuiteDescription: testLaneName,
		SuiteSucceeded:   false,
		SpecReports:      []types.SpecReport{specReport(types.SpecStatePassed, "encodes calls")},
	})

	require.Equal(t, VerdictInfrastructure, summary.Verdict)
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
			summary := summarizeTestLane(test.err,
				suiteReport(testLaneName,
					specReport(types.SpecStatePassed, "encodes calls"),
					specReport(test.state, "decodes events"),
				),
			)

			require.Equal(t, VerdictSkipped, summary.Verdict)
			require.Equal(t, []string{"ABI decodes events"}, summary.UnexpectedSkips)
		})
	}
}

func TestSummarizeHonorsBootstrapFailure(t *testing.T) {
	summary := summarizeOutcome(Outcome{
		Name:            testLaneName,
		Err:             errors.New("network bootstrap failed: start network: no capacity"),
		BootstrapFailed: true,
	})

	require.Equal(t, VerdictBootstrap, summary.Verdict)
	require.NotEmpty(t, summary.Error)
}

func TestSummarizeCountsSetupFailures(t *testing.T) {
	setup := types.SpecReport{
		LeafNodeType: types.NodeTypeSynchronizedBeforeSuite,
		State:        types.SpecStateFailed,
		Failure:      types.Failure{Message: "wallet not funded"},
	}
	summary := summarizeTestLane(errors.New("exit status 1"),
		suiteReport(testLaneName, setup, specReport(types.SpecStateSkipped, "decodes events")),
	)

	require.Equal(t, VerdictAssertion, summary.Verdict)
	require.Len(t, summary.suites[0].Failures, 1)
	require.Equal(t, Counts{Specs: 1, Skipped: 1}, summary.Counts,
		"setup nodes are not specs; the skipped spec still counts")
}

func TestSummarizeTreatsLifecycleFailureAsInfrastructure(t *testing.T) {
	summary := summarizeOutcome(Outcome{
		Name: testLaneName,
		Err:  errors.New("stop network: context deadline exceeded"),
		reports: []types.Report{
			suiteReport(testLaneName, specReport(types.SpecStatePassed, "encodes calls")),
		},
	})

	require.Equal(t, VerdictInfrastructure, summary.Verdict)
	require.Equal(t, "stop network: context deadline exceeded", summary.Error)
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
	summary := summarizeOutcome(executionOutcome("execution", errors.New("exit status 1"),
		suiteReport("ABI E2E suite", specReport(types.SpecStatePassed, "encodes calls")),
		suiteReport("API E2E suite", specReport(types.SpecStateFailed, "decodes events")),
	))

	require.Equal(t, VerdictAssertion, summary.Verdict)
	require.Equal(t, Counts{Specs: 2, Passed: 1, Failed: 1}, summary.Counts)
	require.Len(t, summary.suites, 2)
	require.Equal(t, "ABI E2E suite", summary.suites[0].Name)
	require.Equal(t, VerdictPassed, summary.suites[0].Verdict)
	require.Equal(t, "API E2E suite", summary.suites[1].Name)
	require.Equal(t, VerdictAssertion, summary.suites[1].Verdict)
}

func TestSummarizeRejectsEmptySuiteWithinLane(t *testing.T) {
	summary := summarizeOutcome(executionOutcome("execution", nil,
		types.Report{SuiteDescription: "Empty E2E suite", SuiteSucceeded: true},
		suiteReport("ABI E2E suite", specReport(types.SpecStatePassed, "encodes calls")),
	))

	require.Equal(t, VerdictInfrastructure, summary.Verdict)
	require.Equal(t, VerdictInfrastructure, summary.suites[0].Verdict)
	require.Equal(t, VerdictPassed, summary.suites[1].Verdict)
}

func TestSummarizeEmptyReportIsInfrastructure(t *testing.T) {
	summary := summarizeTestLane(nil, suiteReport(testLaneName))
	require.Equal(t, VerdictInfrastructure, summary.Verdict)
}

func TestVerdictError(t *testing.T) {
	summary := Summary{Lanes: []LaneSummary{{Name: "abi", Verdict: VerdictPassed}}}
	require.NoError(t, summary.VerdictError())

	summary.Lanes = append(summary.Lanes, LaneSummary{
		Name:    "sync",
		Verdict: VerdictInfrastructure,
		Error:   "exit status 1",
	})
	require.EqualError(t, summary.VerdictError(), "lanes did not pass: sync (infrastructure): exit status 1")
}
