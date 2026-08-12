package results

import (
	"encoding/json"
	"errors"
	"fmt"
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

func summarizeOne(t *testing.T, root string, lane Lane) Summary {
	t.Helper()
	summary, err := Summarize(root, []Lane{lane})
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

	summary := summarizeOne(t, root, Lane{Name: "execution-abi", ReportDir: laneDir})
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

	summary := summarizeOne(t, root, Lane{
		Name:      "execution-abi",
		ReportDir: laneDir,
		Err:       errors.New("exit status 1"),
	})
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

	summary := summarizeOne(t, root, Lane{
		Name:      "execution-abi",
		ReportDir: laneDir,
		Err:       errors.New("exit status 1"),
	})
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

	summary := summarizeOne(t, root, Lane{Name: "execution-abi", ReportDir: laneDir})
	require.Equal(t, "failed", summary.Result)
	require.Equal(t, ClassSkipped, summary.Lanes[0].Class)
	require.ErrorContains(t, summary.VerdictError(), "execution-abi (skipped)")
}

func TestSummarizeClassifiesSentinels(t *testing.T) {
	for class, laneErr := range map[string]error{
		ClassBootstrap:      fmt.Errorf("%w: start network: no capacity", ErrBootstrap),
		ClassInfrastructure: fmt.Errorf("%w: create report directory: denied", ErrInfrastructure),
	} {
		t.Run(class, func(t *testing.T) {
			root := t.TempDir()
			summary := summarizeOne(t, root, Lane{
				Name:      "execution-abi",
				ReportDir: filepath.Join(root, "lanes", "execution-abi"),
				Err:       laneErr,
			})
			require.Equal(t, "failed", summary.Result)
			require.Equal(t, class, summary.Lanes[0].Class)
			require.NotEmpty(t, summary.Lanes[0].Error)
		})
	}
}

func TestSummarizeMissingReportIsInfrastructure(t *testing.T) {
	root := t.TempDir()
	summary := summarizeOne(t, root, Lane{
		Name:      "execution-abi",
		ReportDir: filepath.Join(root, "lanes", "execution-abi"),
		Err:       errors.New("exit status 1"),
	})
	require.Equal(t, ClassInfrastructure, summary.Lanes[0].Class,
		"without a report nothing distinguishes a product failure from a broken harness")
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

	summary := summarizeOne(t, root, Lane{
		Name:      "execution-abi",
		ReportDir: laneDir,
		Err:       errors.New("exit status 1"),
	})
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

	summary, err := Summarize(root, []Lane{
		{Name: "execution-abi", ReportDir: passedDir},
		{Name: "consensus", ReportDir: failedDir, Err: errors.New("exit status 1")},
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

	summary := summarizeOne(t, root, Lane{Name: "execution-abi", ReportDir: laneDir})
	require.Equal(t, ClassInfrastructure, summary.Lanes[0].Class)
}

func TestVerdictError(t *testing.T) {
	summary := Summary{Lanes: []LaneSummary{{Name: "abi", Class: ClassPassed}}}
	require.NoError(t, summary.VerdictError())

	summary.Lanes = append(summary.Lanes, LaneSummary{Name: "sync", Class: ClassInfrastructure})
	require.ErrorContains(t, summary.VerdictError(), "sync (infrastructure)")
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
	lane := Lane{Name: "execution-abi", ReportDir: laneDir, Err: errors.New("exit status 1")}
	summary := summarizeOne(t, root, lane)
	require.Equal(t, ClassSkipped, summary.Lanes[0].Class)
}
