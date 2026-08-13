package results

import (
	"fmt"
	"strings"
)

// Markdown renders the summary for the GitHub job summary; workflows append
// it to $GITHUB_STEP_SUMMARY.
func (summary Summary) Markdown() string {
	var report strings.Builder
	fmt.Fprintf(&report, "# E2E result: %s\n\n", summary.Result)
	report.WriteString("| Lane | Class | Specs | Passed | Failed | Pending | Skipped |\n")
	report.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, lane := range summary.Lanes {
		writeCountsRow(&report, lane.Name, lane.Class, lane.Counts)
	}
	if len(summary.Lanes) > 1 {
		writeCountsRow(&report, "total", summary.Result, summary.Totals)
	}

	for _, lane := range summary.Lanes {
		if len(lane.Failures) == 0 && len(lane.SuiteFailures) == 0 && len(lane.UnexpectedSkips) == 0 && lane.Error == "" {
			continue
		}
		fmt.Fprintf(&report, "\n## %s\n\n", lane.Name)
		if lane.Error != "" {
			fmt.Fprintf(&report, "```\n%s\n```\n", lane.Error)
		}
		for _, failure := range lane.Failures {
			fmt.Fprintf(&report, "- **%s** `%s`", failure.State, failure.Spec)
			if failure.Location != "" {
				fmt.Fprintf(&report, " (%s)", failure.Location)
			}
			if failure.Message != "" {
				fmt.Fprintf(&report, "\n  %s", indent(failure.Message))
			}
			report.WriteString("\n")
		}
		for _, failure := range lane.SuiteFailures {
			fmt.Fprintf(&report, "- **suite** %s\n", failure)
		}
		for _, skipped := range lane.UnexpectedSkips {
			fmt.Fprintf(&report, "- **skipped** `%s`\n", skipped)
		}
	}
	return report.String()
}

func writeCountsRow(report *strings.Builder, name, class string, counts Counts) {
	fmt.Fprintf(report, "| %s | %s | %d | %d | %d | %d | %d |\n",
		name, class, counts.Specs, counts.Passed, counts.Failed, counts.Pending, counts.Skipped)
}

func indent(message string) string {
	return strings.ReplaceAll(strings.TrimSpace(message), "\n", "\n  ")
}
