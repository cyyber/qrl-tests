package results

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/ginkgo/v2/types"
	"github.com/stretchr/testify/require"
)

func spec(state types.SpecState, text string) types.SpecReport {
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

func writeReport(t *testing.T, laneDir string, specs ...types.SpecReport) {
	t.Helper()
	payload, err := json.Marshal([]types.Report{{SuitePath: laneDir, SpecReports: specs}})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(laneDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(laneDir, "report.json"), payload, 0o600))
}

func observedOutcome(name, reportDir string, err error) Outcome {
	return Outcome{Name: name, Observation: Observe(reportDir), Err: err}
}

func summarizeOne(t *testing.T, root string, outcome Outcome) Summary {
	t.Helper()
	summary, err := Summarize(root, []Outcome{outcome})
	require.NoError(t, err)
	return summary
}

func TestSummarizePassedLane(t *testing.T) {
	root := t.TempDir()
	laneDir := filepath.Join(root, "lanes", "execution-abi")
	writeReport(t, laneDir,
		spec(types.SpecStatePassed, "encodes calls"),
		spec(types.SpecStatePassed, "decodes events"),
	)

	outcome := observedOutcome("execution-abi", laneDir, nil)
	require.True(t, outcome.Passed())
	summary := summarizeOne(t, root, outcome)
	require.Equal(t, "passed", summary.Result)
	require.Equal(t, ClassPassed, summary.Lanes[0].Class)
	require.Equal(t, Counts{Specs: 2, Passed: 2}, summary.Lanes[0].Counts)

	var written Summary
	payload, err := os.ReadFile(filepath.Join(root, SummaryFileName))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(payload, &written))
	require.Equal(t, summary, written)

	markdown, err := os.ReadFile(filepath.Join(root, MarkdownFileName))
	require.NoError(t, err)
	require.Contains(t, string(markdown), "# E2E result: passed")
	require.Contains(t, string(markdown), "| execution-abi | passed | 2 | 2 | 0 | 0 | 0 |")
}

func TestSummarizeClassifiesAssertionFailures(t *testing.T) {
	root := t.TempDir()
	laneDir := filepath.Join(root, "lanes", "execution-abi")
	writeReport(t, laneDir,
		spec(types.SpecStatePassed, "encodes calls"),
		spec(types.SpecStateFailed, "decodes events"),
	)

	summary := summarizeOne(t, root, observedOutcome("execution-abi", laneDir, errors.New("exit status 1")))
	require.Equal(t, "failed", summary.Result)
	lane := summary.Lanes[0]
	require.Equal(t, ClassAssertion, lane.Class)
	require.Equal(t, Counts{Specs: 2, Passed: 1, Failed: 1}, lane.Counts)
	require.Len(t, lane.Failures, 1)
	require.Equal(t, "ABI decodes events", lane.Failures[0].Spec)
	require.Equal(t, "expected 1, got 2", lane.Failures[0].Message)
	require.Equal(t, "calls_test.go:12", lane.Failures[0].Location)
}

func TestSummarizeClassifiesTimeouts(t *testing.T) {
	root := t.TempDir()
	laneDir := filepath.Join(root, "lanes", "execution-abi")
	writeReport(t, laneDir,
		spec(types.SpecStateFailed, "decodes events"),
		spec(types.SpecStateTimedout, "streams blocks"),
	)

	summary := summarizeOne(t, root, observedOutcome("execution-abi", laneDir, errors.New("exit status 1")))
	require.Equal(t, ClassTimeout, summary.Lanes[0].Class,
		"a lane that hit its deadline is a timeout even when other specs failed")
}

func TestSummarizeRejectsUnexpectedSkips(t *testing.T) {
	root := t.TempDir()
	laneDir := filepath.Join(root, "lanes", "execution-abi")
	writeReport(t, laneDir,
		spec(types.SpecStatePassed, "encodes calls"),
		spec(types.SpecStateSkipped, "decodes events"),
	)

	summary := summarizeOne(t, root, observedOutcome("execution-abi", laneDir, nil))
	require.Equal(t, "failed", summary.Result)
	require.Equal(t, ClassSkipped, summary.Lanes[0].Class)
	require.ErrorContains(t, summary.VerdictError(), "execution-abi (skipped)")
}

func TestSummarizeHonorsBootstrapFailure(t *testing.T) {
	root := t.TempDir()
	summary := summarizeOne(t, root, Outcome{
		Name:             "execution-abi",
		Err:              errors.New("network bootstrap failed: start network: no capacity"),
		BootstrapFailure: true,
	})
	require.Equal(t, "failed", summary.Result)
	require.Equal(t, ClassBootstrap, summary.Lanes[0].Class)
	require.NotEmpty(t, summary.Lanes[0].Error)
}

func TestSummarizeMissingReportIsInfrastructure(t *testing.T) {
	root := t.TempDir()
	summary := summarizeOne(t, root, observedOutcome(
		"execution-abi",
		filepath.Join(root, "lanes", "execution-abi"),
		errors.New("exit status 1"),
	))
	lane := summary.Lanes[0]
	require.Equal(t, ClassInfrastructure, lane.Class,
		"without a report nothing distinguishes a product failure from a broken harness")
	require.Contains(t, lane.Error, "exit status 1")
	require.Contains(t, lane.Error, "report.json")
	require.Contains(t, lane.Error, "no such file or directory")
}

func TestSummarizeCorruptReportIncludesDecodeError(t *testing.T) {
	root := t.TempDir()
	laneDir := filepath.Join(root, "lanes", "execution-abi")
	require.NoError(t, os.MkdirAll(laneDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(laneDir, "report.json"), []byte("{"), 0o600))

	summary := summarizeOne(t, root, observedOutcome("execution-abi", laneDir, nil))
	lane := summary.Lanes[0]
	require.Equal(t, ClassInfrastructure, lane.Class)
	require.Contains(t, lane.Error, "decode ")
	require.Contains(t, lane.Error, "report.json")
}

func TestSummarizeCountsSetupFailures(t *testing.T) {
	root := t.TempDir()
	laneDir := filepath.Join(root, "lanes", "execution-abi")
	setup := types.SpecReport{
		LeafNodeType: types.NodeTypeSynchronizedBeforeSuite,
		State:        types.SpecStateFailed,
		Failure:      types.Failure{Message: "wallet not funded"},
	}
	writeReport(t, laneDir, setup, spec(types.SpecStateSkipped, "decodes events"))

	summary := summarizeOne(t, root, observedOutcome("execution-abi", laneDir, errors.New("exit status 1")))
	lane := summary.Lanes[0]
	require.Equal(t, ClassAssertion, lane.Class)
	require.Len(t, lane.Failures, 1)
	require.Equal(t, Counts{Specs: 1, Skipped: 1}, lane.Counts,
		"setup nodes are not specs; the skipped spec still counts")
}

func TestSummarizeAggregatesLanes(t *testing.T) {
	root := t.TempDir()
	passedDir := filepath.Join(root, "lanes", "execution-abi")
	failedDir := filepath.Join(root, "lanes", "consensus")
	writeReport(t, passedDir, spec(types.SpecStatePassed, "encodes calls"))
	writeReport(t, failedDir, spec(types.SpecStateFailed, "finalizes"))

	summary, err := Summarize(root, []Outcome{
		observedOutcome("execution-abi", passedDir, nil),
		observedOutcome("consensus", failedDir, errors.New("exit status 1")),
	})
	require.NoError(t, err)
	require.Equal(t, "failed", summary.Result)
	require.Equal(t, Counts{Specs: 2, Passed: 1, Failed: 1}, summary.Totals)

	markdown, err := os.ReadFile(filepath.Join(root, MarkdownFileName))
	require.NoError(t, err)
	require.Contains(t, string(markdown), "| total | failed | 2 | 1 | 1 | 0 | 0 |")
}

func TestSummarizeEmptyReportIsInfrastructure(t *testing.T) {
	root := t.TempDir()
	laneDir := filepath.Join(root, "lanes", "execution-abi")
	writeReport(t, laneDir)

	summary := summarizeOne(t, root, observedOutcome("execution-abi", laneDir, nil))
	require.Equal(t, ClassInfrastructure, summary.Lanes[0].Class)
}

func TestVerdictError(t *testing.T) {
	summary := Summary{Lanes: []LaneSummary{{Name: "abi", Class: ClassPassed}}}
	require.NoError(t, summary.VerdictError())

	summary.Lanes = append(summary.Lanes, LaneSummary{
		Name:  "sync",
		Class: ClassInfrastructure,
		Error: "exit status 1",
	})
	require.ErrorContains(t, summary.VerdictError(), "sync (infrastructure)")
	require.ErrorContains(t, summary.VerdictError(), "exit status 1")
}

func TestSkippedSpecsOutrankTheExitCode(t *testing.T) {
	root := t.TempDir()
	laneDir := filepath.Join(root, "lanes", "execution-abi")
	writeReport(t, laneDir,
		spec(types.SpecStatePassed, "encodes calls"),
		spec(types.SpecStatePending, "decodes events"),
	)

	// --fail-on-pending makes ginkgo exit nonzero for a pending spec; the
	// classification must still come from the report, not the exit code.
	outcome := observedOutcome("execution-abi", laneDir, errors.New("exit status 1"))
	summary := summarizeOne(t, root, outcome)
	require.Equal(t, ClassSkipped, summary.Lanes[0].Class)
}

func TestSummarizeUsesTheCapturedObservation(t *testing.T) {
	root := t.TempDir()
	laneDir := filepath.Join(root, "lanes", "execution-abi")
	writeReport(t, laneDir, spec(types.SpecStatePassed, "encodes calls"))
	outcome := observedOutcome("execution-abi", laneDir, nil)

	writeReport(t, laneDir, spec(types.SpecStateFailed, "encodes calls"))
	summary := summarizeOne(t, root, outcome)
	require.Equal(t, ClassPassed, summary.Lanes[0].Class)
}
