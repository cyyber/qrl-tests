package results

import (
	"fmt"
	"strings"
)

// markdown renders the summary for the GitHub job summary; workflows append
// it to $GITHUB_STEP_SUMMARY.
func (summary Summary) markdown() string {
	var report strings.Builder
	fmt.Fprintf(&report, "## E2E: %s\n", summary.Result)
	for _, lane := range summary.Lanes {
		writeLaneSummary(&report, lane)
	}

	for _, lane := range summary.Lanes {
		writeLaneDetails(&report, lane)
	}
	return report.String()
}

func writeLaneSummary(report *strings.Builder, lane LaneSummary) {
	fmt.Fprintf(report, "\n### %s\n\n", lane.Name)
	if len(lane.suites) == 0 {
		fmt.Fprintf(report, "**Result:** %s\n", displayClass(lane.Class))
		return
	}
	report.WriteString("| Suite | Result |\n")
	report.WriteString("| --- | ---: |\n")
	for _, suite := range lane.suites {
		fmt.Fprintf(report, "| %s | %s |\n", suite.Name, suiteResult(suite))
	}
}

func writeLaneDetails(report *strings.Builder, lane LaneSummary) {
	if showLaneError(lane) {
		fmt.Fprintf(report, "\n### %s details\n\n", lane.Name)
		fmt.Fprintf(report, "```\n%s\n```\n", lane.Error)
	}
	for _, suite := range lane.suites {
		if !suite.hasDetails() {
			continue
		}
		fmt.Fprintf(report, "\n#### %s failures\n\n", suite.Name)
		writeSuiteDetails(report, suite)
	}
}

func (suite suiteSummary) hasDetails() bool {
	return len(suite.Failures) > 0 || len(suite.SuiteFailures) > 0 || len(suite.UnexpectedSkips) > 0
}

func writeSuiteDetails(report *strings.Builder, suite suiteSummary) {
	for _, failure := range suite.Failures {
		fmt.Fprintf(report, "- **%s** `%s`", failure.State, failure.Spec)
		if failure.Location != "" {
			fmt.Fprintf(report, " (%s)", failure.Location)
		}
		if failure.Message != "" {
			fmt.Fprintf(report, "\n  %s", indentMarkdown(failure.Message))
		}
		report.WriteString("\n")
	}
	for _, failure := range suite.SuiteFailures {
		fmt.Fprintf(report, "- **suite** %s\n", failure)
	}
	for _, skipped := range suite.UnexpectedSkips {
		fmt.Fprintf(report, "- **skipped** `%s`\n", skipped)
	}
}

func suiteResult(suite suiteSummary) string {
	result := fmt.Sprintf("%d/%d", suite.Counts.Passed, suite.Counts.Specs)
	if suite.Class != ClassPassed {
		result += " " + displayClass(suite.Class)
	}
	return result
}

func displayClass(class string) string {
	switch class {
	case ClassAssertion:
		return "failed"
	case ClassTimeout:
		return "timed out"
	case ClassInfrastructure:
		return "error"
	default:
		return class
	}
}

func showLaneError(lane LaneSummary) bool {
	return lane.Error != "" && lane.Class != ClassAssertion && lane.Class != ClassSkipped
}

func indentMarkdown(message string) string {
	return strings.ReplaceAll(strings.TrimSpace(message), "\n", "\n  ")
}
