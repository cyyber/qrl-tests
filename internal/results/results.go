// Package results turns per-lane Ginkgo reports into the run's verdict:
// spec counts, classified failures, and the summary files CI publishes.
package results

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/onsi/ginkgo/v2/types"
)

const (
	SummaryFileName  = "summary.json"
	MarkdownFileName = "summary.md"
)

// The runner tags lane errors with these sentinels so failures that happened
// before Ginkgo could run are classified without consulting spec reports.
var (
	ErrBootstrap      = errors.New("network bootstrap failed")
	ErrInfrastructure = errors.New("test infrastructure failed")
)

// Classifications, from most to least fundamental: a lane gets the first one
// that applies.
const (
	ClassBootstrap      = "bootstrap"      // the network never became ready
	ClassInfrastructure = "infrastructure" // the harness or its tools broke
	ClassTimeout        = "timeout"        // specs hit the lane deadline
	ClassAssertion      = "assertion"      // the product failed a spec
	ClassSkipped        = "skipped"        // only unexpected skips or pendings
	ClassPassed         = "passed"
)

// Lane pairs a finished lane with the error its run returned.
type Lane struct {
	Name      string
	ReportDir string
	Err       error
}

type Counts struct {
	Specs   int `json:"specs"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Pending int `json:"pending"`
	Skipped int `json:"skipped"`
}

type Failure struct {
	Spec     string `json:"spec"`
	State    string `json:"state"`
	Message  string `json:"message,omitempty"`
	Location string `json:"location,omitempty"`
}

type LaneSummary struct {
	Name            string    `json:"name"`
	Class           string    `json:"class"`
	Counts          Counts    `json:"counts"`
	Error           string    `json:"error,omitempty"`
	Failures        []Failure `json:"failures,omitempty"`
	UnexpectedSkips []string  `json:"unexpected_skips,omitempty"`
}

type Summary struct {
	Result string        `json:"result"`
	Totals Counts        `json:"totals"`
	Lanes  []LaneSummary `json:"lanes"`
}

// Summarize reads every lane's Ginkgo JSON report, writes summary.json and
// summary.md under reportRoot, and returns the assembled summary. The
// returned error covers reading and writing only; test failures live in the
// summary itself and in SkipError.
func Summarize(reportRoot string, lanes []Lane) (Summary, error) {
	summary := Summary{Result: "passed", Lanes: make([]LaneSummary, len(lanes))}
	for index, lane := range lanes {
		summary.Lanes[index] = summarizeLane(lane)

		counts := summary.Lanes[index].Counts
		summary.Totals.Specs += counts.Specs
		summary.Totals.Passed += counts.Passed
		summary.Totals.Failed += counts.Failed
		summary.Totals.Pending += counts.Pending
		summary.Totals.Skipped += counts.Skipped
		if summary.Lanes[index].Class != ClassPassed {
			summary.Result = "failed"
		}
	}

	// The assembled summary always comes back, even when persisting it
	// fails: the verdict must never degrade to process status because a
	// file could not be written.
	payload, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return summary, fmt.Errorf("encode summary: %w", err)
	}
	if err := os.MkdirAll(reportRoot, 0o755); err != nil {
		return summary, fmt.Errorf("create report directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(reportRoot, SummaryFileName), append(payload, '\n'), 0o600); err != nil {
		return summary, fmt.Errorf("write summary: %w", err)
	}
	if err := os.WriteFile(filepath.Join(reportRoot, MarkdownFileName), []byte(summary.Markdown()), 0o600); err != nil {
		return summary, fmt.Errorf("write markdown summary: %w", err)
	}
	return summary, nil
}

// LaneHealthy reports whether the report on disk describes a fully passing
// lane. This is the pre-cleanup verdict: it lets diagnostics run while the
// network still exists, instead of discovering a bad report after the
// enclave is gone.
func LaneHealthy(reportDir string) bool {
	return summarizeLane(Lane{ReportDir: reportDir}).Class == ClassPassed
}

// VerdictError converts a non-passing summary into an error, so the process
// exit can never contradict the published verdict: a lane that exited
// cleanly but produced no usable report still fails the run.
func (summary Summary) VerdictError() error {
	var failed []string
	for _, lane := range summary.Lanes {
		if lane.Class != ClassPassed {
			failed = append(failed, fmt.Sprintf("%s (%s)", lane.Name, lane.Class))
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return fmt.Errorf("lanes did not pass: %s", strings.Join(failed, "; "))
}

// SkipError reports unexpected skipped or pending specs as an error, so a run
// whose lanes all "succeeded" by exit code still fails on silent skips.
func (summary Summary) SkipError() error {
	var skipped []string
	for _, lane := range summary.Lanes {
		skipped = append(skipped, lane.UnexpectedSkips...)
	}
	if len(skipped) == 0 {
		return nil
	}
	return fmt.Errorf("unexpected skipped or pending specs: %s", strings.Join(skipped, "; "))
}

func summarizeLane(lane Lane) LaneSummary {
	result := LaneSummary{Name: lane.Name, Class: ClassPassed}
	if lane.Err != nil {
		result.Error = lane.Err.Error()
	}

	reports, reportErr := readReports(filepath.Join(lane.ReportDir, "report.json"))
	for _, report := range reports {
		for _, spec := range report.SpecReports {
			result.tally(spec)
		}
	}

	result.Class = classify(lane, reports, reportErr, result)
	return result
}

func (lane *LaneSummary) tally(spec types.SpecReport) {
	name := strings.Join(append(append([]string{}, spec.ContainerHierarchyTexts...), spec.LeafNodeText), " ")
	if name == "" {
		name = spec.LeafNodeType.String()
	}

	if spec.LeafNodeType == types.NodeTypeIt {
		lane.Counts.Specs++
		switch spec.State {
		case types.SpecStatePassed:
			lane.Counts.Passed++
		case types.SpecStatePending:
			lane.Counts.Pending++
			lane.UnexpectedSkips = append(lane.UnexpectedSkips, name)
		case types.SpecStateSkipped:
			lane.Counts.Skipped++
			lane.UnexpectedSkips = append(lane.UnexpectedSkips, name)
		default:
			lane.Counts.Failed++
		}
	}

	if spec.State.Is(types.SpecStateFailureStates) {
		failure := Failure{Spec: name, State: spec.State.String(), Message: spec.Failure.Message}
		if location := spec.Failure.Location.FileName; location != "" {
			failure.Location = fmt.Sprintf("%s:%d", location, spec.Failure.Location.LineNumber)
		}
		lane.Failures = append(lane.Failures, failure)
	}
}

func classify(lane Lane, reports []types.Report, reportErr error, tallied LaneSummary) string {
	switch {
	case errors.Is(lane.Err, ErrBootstrap):
		return ClassBootstrap
	case errors.Is(lane.Err, ErrInfrastructure):
		return ClassInfrastructure
	}

	// Without a readable report, nothing distinguishes a product failure from
	// a broken harness — and a passing lane always has a report.
	if reportErr != nil || len(reports) == 0 {
		return ClassInfrastructure
	}

	timedOut := false
	for _, failure := range tallied.Failures {
		switch failure.State {
		case types.SpecStateTimedout.String(), types.SpecStateInterrupted.String():
			timedOut = true
		}
	}
	switch {
	case timedOut:
		return ClassTimeout
	case tallied.Counts.Failed > 0 || len(tallied.Failures) > 0:
		return ClassAssertion
	case len(tallied.UnexpectedSkips) > 0:
		// Report-backed evidence outranks the bare exit code: ginkgo runs
		// with --fail-on-pending, so a pending or skipped spec also fails
		// the process, and that exit says nothing new.
		return ClassSkipped
	case lane.Err != nil:
		// The test process failed without a failing spec on record.
		return ClassInfrastructure
	case tallied.Counts.Passed == 0:
		// An empty report means the suites never ran.
		return ClassInfrastructure
	}
	return ClassPassed
}

func readReports(path string) ([]types.Report, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var reports []types.Report
	if err := json.Unmarshal(payload, &reports); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return reports, nil
}
