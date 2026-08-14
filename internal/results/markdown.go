package results

import (
	"fmt"
	"strings"

	md "github.com/nao1215/markdown"
)

// markdown renders the summary for the GitHub job summary; workflows append
// it to $GITHUB_STEP_SUMMARY.
func (summary Summary) markdown() string {
	report := md.NewMarkdown(nil).H2f("E2E: %s", summary.Result).PlainText("")
	for _, lane := range summary.Lanes {
		writeLaneSummary(report, lane)
	}

	for _, lane := range summary.Lanes {
		writeLaneDetails(report, lane)
	}
	return report.String()
}

func writeLaneSummary(report *md.Markdown, lane LaneSummary) {
	report.H3(lane.Name).PlainText("")
	if len(lane.suites) == 0 {
		report.PlainTextf("%s %s", md.Bold("Result:"), displayVerdict(lane.Verdict)).PlainText("")
		return
	}

	var table strings.Builder
	table.WriteString("| Suite | Result |\n|---------|--------:|")
	for _, suite := range lane.suites {
		fmt.Fprintf(&table, "\n| %s | %s |", suite.Name, suiteResult(suite))
	}
	report.PlainText(table.String()).PlainText("")
}

func writeLaneDetails(report *md.Markdown, lane LaneSummary) {
	if showLaneError(lane) {
		report.H3f("%s details", lane.Name).PlainText("")
		report.CodeBlocks(md.SyntaxHighlightNone, lane.Error)
	}
	for _, suite := range lane.suites {
		if !suite.hasDetails() {
			continue
		}
		report.H4f("%s failures", suite.Name).PlainText("")
		report.BulletList(suiteDetails(suite)...)
	}
}

func (suite suiteSummary) hasDetails() bool {
	return len(suite.Failures) > 0 || len(suite.SuiteFailures) > 0 || len(suite.UnexpectedSkips) > 0
}

func suiteDetails(suite suiteSummary) []string {
	details := make([]string, 0, len(suite.Failures)+len(suite.SuiteFailures)+len(suite.UnexpectedSkips))
	for _, failure := range suite.Failures {
		detail := fmt.Sprintf("%s %s", md.Bold(failure.State), md.Code(failure.Spec))
		if failure.Location != "" {
			detail += fmt.Sprintf(" (%s)", failure.Location)
		}
		if failure.Message != "" {
			detail += "\n  " + indentListContinuation(failure.Message)
		}
		details = append(details, detail)
	}
	for _, failure := range suite.SuiteFailures {
		details = append(details, fmt.Sprintf("%s %s", md.Bold("suite"), failure))
	}
	for _, skipped := range suite.UnexpectedSkips {
		details = append(details, fmt.Sprintf("%s %s", md.Bold("skipped"), md.Code(skipped)))
	}
	return details
}

func suiteResult(suite suiteSummary) string {
	result := fmt.Sprintf("%d/%d", suite.Counts.Passed, suite.Counts.Specs)
	if suite.Verdict != VerdictPassed {
		result += " " + displayVerdict(suite.Verdict)
	}
	return result
}

func displayVerdict(verdict string) string {
	switch verdict {
	case VerdictAssertion:
		return "failed"
	case VerdictTimeout:
		return "timed out"
	case VerdictInfrastructure:
		return "error"
	default:
		return verdict
	}
}

func showLaneError(lane LaneSummary) bool {
	return lane.Error != "" && lane.Verdict != VerdictAssertion && lane.Verdict != VerdictSkipped
}

func indentListContinuation(message string) string {
	return strings.ReplaceAll(strings.TrimSpace(message), "\n", "\n  ")
}
