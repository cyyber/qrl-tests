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

	"github.com/cyyber/qrl-tests/internal/jsonfile"
	"github.com/onsi/ginkgo/v2/types"
)

const (
	ReportFileName   = "report.json"
	SummaryFileName  = "summary.json"
	MarkdownFileName = "summary.md"
)

// Lane verdicts.
const (
	VerdictBootstrap      = "bootstrap"      // the network never became ready
	VerdictInfrastructure = "infrastructure" // the harness or its tools broke
	VerdictTimeout        = "timeout"        // specs hit the lane deadline
	VerdictCanceled       = "canceled"       // the caller interrupted the run
	VerdictAssertion      = "assertion"      // the product failed a spec
	VerdictSkipped        = "skipped"        // only unexpected skips or pendings
	VerdictPassed         = "passed"
)

// Outcome is the complete result of running one lane. ExecutionErr preserves
// the test process error separately from lifecycle failures such as cleanup.
// Captured reports remain private because only result classification consumes
// them.
type Outcome struct {
	Name            string
	Err             error
	ExecutionErr    error
	BootstrapFailed bool
	reports         []types.Report
	reportErr       error
}

// CaptureReports reads and stores the lane's Ginkgo reports so later summary
// generation consumes the same snapshot.
func (outcome *Outcome) CaptureReports(reportDir string) {
	outcome.reports, outcome.reportErr = nil, nil

	path := filepath.Join(reportDir, ReportFileName)
	payload, err := os.ReadFile(path)
	if err != nil {
		outcome.reportErr = err
		return
	}

	var reports []types.Report
	if err := json.Unmarshal(payload, &reports); err != nil {
		outcome.reportErr = fmt.Errorf("decode %s: %w", path, err)
		return
	}

	outcome.reports = reports
}

// Passed reports the verdict represented by this outcome without reading any
// files or discarding its process error.
func (outcome Outcome) Passed() bool {
	return summarizeOutcome(outcome).Verdict == VerdictPassed
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

type classificationEvidence struct {
	counts          Counts
	failures        []Failure
	unexpectedSkips []string
}

type suiteSummary struct {
	Name            string
	Verdict         string
	Counts          Counts
	Failures        []Failure
	SuiteFailures   []string
	UnexpectedSkips []string
}

func (summary suiteSummary) classificationEvidence() classificationEvidence {
	return classificationEvidence{
		counts:          summary.Counts,
		failures:        summary.Failures,
		unexpectedSkips: summary.UnexpectedSkips,
	}
}

type LaneSummary struct {
	Name            string    `json:"name"`
	Verdict         string    `json:"verdict"`
	Counts          Counts    `json:"counts"`
	Error           string    `json:"error,omitempty"`
	Failures        []Failure `json:"failures,omitempty"`
	SuiteFailures   []string  `json:"suite_failures,omitempty"`
	UnexpectedSkips []string  `json:"unexpected_skips,omitempty"`
	suites          []suiteSummary
}

func (summary LaneSummary) classificationEvidence() classificationEvidence {
	return classificationEvidence{
		counts:          summary.Counts,
		failures:        summary.Failures,
		unexpectedSkips: summary.UnexpectedSkips,
	}
}

type Summary struct {
	Result string        `json:"result"`
	Totals Counts        `json:"totals"`
	Lanes  []LaneSummary `json:"lanes"`
}

// VerdictError converts a non-passing summary into an error, so the process
// exit can never contradict the published verdict: a lane that exited
// cleanly but produced no usable report still fails the run.
func (summary Summary) VerdictError() error {
	var failed []string
	for _, lane := range summary.Lanes {
		if lane.Verdict != VerdictPassed {
			detail := fmt.Sprintf("%s (%s)", lane.Name, lane.Verdict)
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

// Summarize writes summary.json and summary.md from the reports captured by
// each outcome. The returned error covers persistence only; test failures live
// in the summary itself and VerdictError.
func Summarize(reportRoot string, outcomes []Outcome) (Summary, error) {
	summary := Summary{Result: "passed", Lanes: make([]LaneSummary, len(outcomes))}
	for index, outcome := range outcomes {
		summary.Lanes[index] = summarizeOutcome(outcome)

		summary.Totals.add(summary.Lanes[index].Counts)
		if summary.Lanes[index].Verdict != VerdictPassed {
			summary.Result = "failed"
		}
	}

	// The assembled summary always comes back, even when persisting it
	// fails: the verdict must never degrade to process status because a
	// file could not be written.
	if err := jsonfile.Write(filepath.Join(reportRoot, SummaryFileName), summary, "summary"); err != nil {
		return summary, err
	}
	if err := os.WriteFile(filepath.Join(reportRoot, MarkdownFileName), []byte(summary.markdown()), 0o600); err != nil {
		return summary, fmt.Errorf("write markdown summary: %w", err)
	}
	return summary, nil
}

func summarizeOutcome(outcome Outcome) LaneSummary {
	result := LaneSummary{Name: outcome.Name, Verdict: VerdictPassed}
	if err := errors.Join(outcome.Err, outcome.reportErr); err != nil {
		result.Error = err.Error()
	}

	for _, report := range outcome.reports {
		suite := summarizeSuite(report)
		result.suites = append(result.suites, suite)
		result.Counts.add(suite.Counts)
		result.Failures = append(result.Failures, suite.Failures...)
		result.SuiteFailures = append(result.SuiteFailures, suite.SuiteFailures...)
		result.UnexpectedSkips = append(result.UnexpectedSkips, suite.UnexpectedSkips...)
	}

	result.Verdict = classify(outcome, result)
	return result
}

func summarizeSuite(report types.Report) suiteSummary {
	result := suiteSummary{
		Name:          suiteName(report),
		Verdict:       VerdictPassed,
		SuiteFailures: report.SpecialSuiteFailureReasons,
	}
	for _, spec := range report.SpecReports {
		result.tally(spec)
	}
	result.Verdict = classifyReports([]types.Report{report}, result.classificationEvidence(), nil)
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

func (summary *suiteSummary) tally(spec types.SpecReport) {
	name := specName(spec)

	if spec.LeafNodeType == types.NodeTypeIt {
		summary.Counts.Specs++
		switch spec.State {
		case types.SpecStatePassed:
			summary.Counts.Passed++
		case types.SpecStatePending:
			summary.Counts.Pending++
			summary.UnexpectedSkips = append(summary.UnexpectedSkips, name)
		case types.SpecStateSkipped:
			summary.Counts.Skipped++
			summary.UnexpectedSkips = append(summary.UnexpectedSkips, name)
		default:
			summary.Counts.Failed++
		}
	}

	if spec.State.Is(types.SpecStateFailureStates) {
		failure := Failure{Spec: name, State: spec.State.String(), Message: spec.Failure.Message}
		if location := spec.Failure.Location.FileName; location != "" {
			failure.Location = fmt.Sprintf("%s:%d", location, spec.Failure.Location.LineNumber)
		}
		summary.Failures = append(summary.Failures, failure)
	}
}

func specName(spec types.SpecReport) string {
	parts := make([]string, 0, len(spec.ContainerHierarchyTexts)+1)
	parts = append(parts, spec.ContainerHierarchyTexts...)
	parts = append(parts, spec.LeafNodeText)
	if name := strings.Join(parts, " "); name != "" {
		return name
	}
	return spec.LeafNodeType.String()
}

func (counts *Counts) add(other Counts) {
	counts.Specs += other.Specs
	counts.Passed += other.Passed
	counts.Failed += other.Failed
	counts.Pending += other.Pending
	counts.Skipped += other.Skipped
}

func classify(outcome Outcome, summary LaneSummary) string {
	if outcome.BootstrapFailed {
		return VerdictBootstrap
	}
	if errors.Is(outcome.ExecutionErr, context.Canceled) {
		return VerdictCanceled
	}
	if errors.Is(outcome.ExecutionErr, context.DeadlineExceeded) {
		return VerdictTimeout
	}

	// Without a readable report, nothing distinguishes a product failure from
	// a broken harness — and a passing lane always has a report.
	if outcome.reportErr != nil || len(outcome.reports) == 0 {
		return VerdictInfrastructure
	}

	classification := classifyReports(
		outcome.reports,
		summary.classificationEvidence(),
		outcome.Err,
	)
	if classification == VerdictPassed {
		for _, suite := range summary.suites {
			if suite.Verdict != VerdictPassed {
				return suite.Verdict
			}
		}
	}
	return classification
}

func classifyReports(reports []types.Report, evidence classificationEvidence, runErr error) string {
	timedOut := anyReportTimedOut(reports)
	interrupted := false
	for _, failure := range evidence.failures {
		switch failure.State {
		case types.SpecStateTimedout.String():
			timedOut = true
		case types.SpecStateInterrupted.String():
			interrupted = true
		}
	}
	switch {
	case timedOut:
		return VerdictTimeout
	case interrupted:
		return VerdictInfrastructure
	case evidence.counts.Failed > 0 || len(evidence.failures) > 0:
		return VerdictAssertion
	case len(evidence.unexpectedSkips) > 0:
		// Report-backed evidence outranks the bare exit code: ginkgo runs
		// with --fail-on-pending, so a pending or skipped spec also fails
		// the process, and that exit says nothing new.
		return VerdictSkipped
	case anyReportFailed(reports):
		return VerdictInfrastructure
	case runErr != nil:
		// The test process failed without a failing spec on record.
		return VerdictInfrastructure
	case evidence.counts.Passed == 0:
		// An empty report means the suites never ran.
		return VerdictInfrastructure
	}
	return VerdictPassed
}

func anyReportTimedOut(reports []types.Report) bool {
	for _, report := range reports {
		for _, reason := range report.SpecialSuiteFailureReasons {
			if strings.Contains(strings.ToLower(reason), "timeout") {
				return true
			}
		}
	}
	return false
}

func anyReportFailed(reports []types.Report) bool {
	for _, report := range reports {
		if !report.SuiteSucceeded {
			return true
		}
	}
	return false
}
