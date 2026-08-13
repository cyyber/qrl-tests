// Package results turns per-lane Ginkgo reports into the run's verdict:
// spec counts, classified failures, and the summary files CI publishes.
package results

import (
	"context"
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

// Classifications, from most to least fundamental: a lane gets the first one
// that applies.
const (
	ClassBootstrap      = "bootstrap"      // the network never became ready
	ClassInfrastructure = "infrastructure" // the harness or its tools broke
	ClassTimeout        = "timeout"        // specs hit the lane deadline
	ClassCanceled       = "canceled"       // the caller interrupted the run
	ClassAssertion      = "assertion"      // the product failed a spec
	ClassSkipped        = "skipped"        // only unexpected skips or pendings
	ClassPassed         = "passed"
)

// Observation is a lane's parsed Ginkgo report. Observe creates it once, while
// the lane still owns its network; summaries then consume the same snapshot.
type Observation struct {
	reports []types.Report
	err     error
}

// Observe reads and decodes the Ginkgo report under reportDir.
func Observe(reportDir string) Observation {
	reports, err := readReports(filepath.Join(reportDir, "report.json"))
	return Observation{reports: reports, err: err}
}

// Outcome is the complete result of running one lane. ExecutionErr preserves
// the test process error separately from lifecycle failures such as cleanup.
type Outcome struct {
	Name             string
	Observation      Observation
	Err              error
	ExecutionErr     error
	BootstrapFailure bool
}

// Passed reports the verdict represented by this outcome without reading any
// files or discarding its process error.
func (outcome Outcome) Passed() bool {
	return summarizeOutcome(outcome).Class == ClassPassed
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

type suiteSummary struct {
	Name            string
	Class           string
	Counts          Counts
	Failures        []Failure
	SuiteFailures   []string
	UnexpectedSkips []string
}

type LaneSummary struct {
	Name            string    `json:"name"`
	Class           string    `json:"class"`
	Counts          Counts    `json:"counts"`
	Error           string    `json:"error,omitempty"`
	Failures        []Failure `json:"failures,omitempty"`
	SuiteFailures   []string  `json:"suite_failures,omitempty"`
	UnexpectedSkips []string  `json:"unexpected_skips,omitempty"`
	suites          []suiteSummary
}

type Summary struct {
	Result string        `json:"result"`
	Totals Counts        `json:"totals"`
	Lanes  []LaneSummary `json:"lanes"`
}

// Summarize writes summary.json and summary.md from the observations captured
// by each outcome. The returned error covers writing only; test failures live
// in the summary itself and VerdictError.
func Summarize(reportRoot string, outcomes []Outcome) (Summary, error) {
	summary := Summary{Result: "passed", Lanes: make([]LaneSummary, len(outcomes))}
	for index, outcome := range outcomes {
		summary.Lanes[index] = summarizeOutcome(outcome)

		summary.Totals.add(summary.Lanes[index].Counts)
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
	if err := os.WriteFile(filepath.Join(reportRoot, MarkdownFileName), []byte(summary.markdown()), 0o600); err != nil {
		return summary, fmt.Errorf("write markdown summary: %w", err)
	}
	return summary, nil
}

// VerdictError converts a non-passing summary into an error, so the process
// exit can never contradict the published verdict: a lane that exited
// cleanly but produced no usable report still fails the run.
func (summary Summary) VerdictError() error {
	var failed []string
	for _, lane := range summary.Lanes {
		if lane.Class != ClassPassed {
			detail := fmt.Sprintf("%s (%s)", lane.Name, lane.Class)
			if lane.Error != "" {
				detail += ": " + lane.Error
			}
			failed = append(failed, detail)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return fmt.Errorf("lanes did not pass: %s", strings.Join(failed, "; "))
}

func summarizeOutcome(outcome Outcome) LaneSummary {
	result := LaneSummary{Name: outcome.Name, Class: ClassPassed}
	if err := errors.Join(outcome.Err, outcome.Observation.err); err != nil {
		result.Error = err.Error()
	}

	for _, report := range outcome.Observation.reports {
		suite := summarizeSuite(report)
		result.suites = append(result.suites, suite)
		result.Counts.add(suite.Counts)
		result.Failures = append(result.Failures, suite.Failures...)
		result.SuiteFailures = append(result.SuiteFailures, suite.SuiteFailures...)
		result.UnexpectedSkips = append(result.UnexpectedSkips, suite.UnexpectedSkips...)
	}

	result.Class = classify(outcome, result)
	return result
}

func summarizeSuite(report types.Report) suiteSummary {
	result := suiteSummary{
		Name:          suiteName(report),
		Class:         ClassPassed,
		SuiteFailures: report.SpecialSuiteFailureReasons,
	}
	for _, spec := range report.SpecReports {
		result.tally(spec)
	}
	result.Class = classifyReports([]types.Report{report}, result.Counts, result.Failures, result.UnexpectedSkips, nil)
	return result
}

func suiteName(report types.Report) string {
	if name := strings.TrimSpace(report.SuiteDescription); name != "" {
		return name
	}
	if path := strings.TrimSpace(report.SuitePath); path != "" {
		return filepath.Base(path)
	}
	return "suite"
}

func (suite *suiteSummary) tally(spec types.SpecReport) {
	name := strings.Join(append(append([]string{}, spec.ContainerHierarchyTexts...), spec.LeafNodeText), " ")
	if name == "" {
		name = spec.LeafNodeType.String()
	}

	if spec.LeafNodeType == types.NodeTypeIt {
		suite.Counts.Specs++
		switch spec.State {
		case types.SpecStatePassed:
			suite.Counts.Passed++
		case types.SpecStatePending:
			suite.Counts.Pending++
			suite.UnexpectedSkips = append(suite.UnexpectedSkips, name)
		case types.SpecStateSkipped:
			suite.Counts.Skipped++
			suite.UnexpectedSkips = append(suite.UnexpectedSkips, name)
		default:
			suite.Counts.Failed++
		}
	}

	if spec.State.Is(types.SpecStateFailureStates) {
		failure := Failure{Spec: name, State: spec.State.String(), Message: spec.Failure.Message}
		if location := spec.Failure.Location.FileName; location != "" {
			failure.Location = fmt.Sprintf("%s:%d", location, spec.Failure.Location.LineNumber)
		}
		suite.Failures = append(suite.Failures, failure)
	}
}

func (counts *Counts) add(other Counts) {
	counts.Specs += other.Specs
	counts.Passed += other.Passed
	counts.Failed += other.Failed
	counts.Pending += other.Pending
	counts.Skipped += other.Skipped
}

func classify(outcome Outcome, summary LaneSummary) string {
	if outcome.BootstrapFailure {
		return ClassBootstrap
	}
	if errors.Is(outcome.ExecutionErr, context.Canceled) {
		return ClassCanceled
	}
	if errors.Is(outcome.ExecutionErr, context.DeadlineExceeded) {
		return ClassTimeout
	}

	// Without a readable report, nothing distinguishes a product failure from
	// a broken harness — and a passing lane always has a report.
	if outcome.Observation.err != nil || len(outcome.Observation.reports) == 0 {
		return ClassInfrastructure
	}

	return classifyReports(
		outcome.Observation.reports,
		summary.Counts,
		summary.Failures,
		summary.UnexpectedSkips,
		outcome.Err,
	)
}

func classifyReports(reports []types.Report, counts Counts, failures []Failure, unexpectedSkips []string, err error) string {
	timedOut := reportTimedOut(reports)
	interrupted := false
	for _, failure := range failures {
		switch failure.State {
		case types.SpecStateTimedout.String():
			timedOut = true
		case types.SpecStateInterrupted.String():
			interrupted = true
		}
	}
	switch {
	case timedOut:
		return ClassTimeout
	case interrupted:
		return ClassInfrastructure
	case counts.Failed > 0 || len(failures) > 0:
		return ClassAssertion
	case len(unexpectedSkips) > 0:
		// Report-backed evidence outranks the bare exit code: ginkgo runs
		// with --fail-on-pending, so a pending or skipped spec also fails
		// the process, and that exit says nothing new.
		return ClassSkipped
	case reportFailed(reports):
		return ClassInfrastructure
	case err != nil:
		// The test process failed without a failing spec on record.
		return ClassInfrastructure
	case counts.Passed == 0:
		// An empty report means the suites never ran.
		return ClassInfrastructure
	}
	return ClassPassed
}

func reportTimedOut(reports []types.Report) bool {
	for _, report := range reports {
		for _, reason := range report.SpecialSuiteFailureReasons {
			if strings.Contains(strings.ToLower(reason), "timeout") {
				return true
			}
		}
	}
	return false
}

func reportFailed(reports []types.Report) bool {
	for _, report := range reports {
		if !report.SuiteSucceeded {
			return true
		}
	}
	return false
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
