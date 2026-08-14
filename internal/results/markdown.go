package results

import (
	"fmt"
	"strings"

	md "github.com/nao1215/markdown"
)

// markdown renders the summary for the GitHub job summary; workflows append
// it to $GITHUB_STEP_SUMMARY.
func (summary Summary) markdown() string {
	report := md.NewMarkdown(nil, md.WithBlockSpacing()).H2f("E2E: %s", summary.Result)
	for _, lane := range summary.Lanes {
		writeLaneSummary(report, lane)
	}

	for _, lane := range summary.Lanes {
		writeLaneDetails(report, lane)
	}
	return strings.TrimRight(report.String(), "\n") + "\n"
}

func writeLaneSummary(report *md.Markdown, lane LaneSummary) {
	report.H3(lane.Name)
	if len(lane.suites) == 0 {
		report.PlainTextf("%s %s", md.Bold("Result:"), displayClass(lane.Class))
		return
	}

	rows := make([][]string, len(lane.suites))
	for i, suite := range lane.suites {
		rows[i] = []string{suite.Name, suiteResult(suite)}
	}
	report.Table(md.TableSet{
		Header:    []string{"Suite", "Result"},
		Rows:      rows,
		Alignment: []md.TableAlignment{md.AlignDefault, md.AlignRight},
	})
}

func writeLaneDetails(report *md.Markdown, lane LaneSummary) {
	if showLaneError(lane) {
		report.H3f("%s details", lane.Name)
		report.CodeBlocks(md.SyntaxHighlightNone, lane.Error)
	}
	for _, suite := range lane.suites {
		if !suite.hasDetails() {
			continue
		}
		report.H4f("%s failures", suite.Name)
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
			detail += "\n  " + indentMarkdown(failure.Message)
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
