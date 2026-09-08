package soak

import (
	"fmt"
	"strings"
)

// VerdictClass is passed, infrastructure, or product. Placement breaches
// count as infrastructure even when SOAK_ENFORCE is off and the run itself
// still passes.
func VerdictClass(evaluation Evaluation) string {
	failed := FailedGates(evaluation)
	if len(failed) == 0 {
		return "passed"
	}
	for _, gate := range failed {
		if strings.HasPrefix(gate.Name, "placement/") {
			return "infrastructure"
		}
	}
	return "product"
}

func FailedGates(evaluation Evaluation) []Gate {
	var failed []Gate
	for _, gate := range evaluation.Gates {
		if !gate.Passed {
			failed = append(failed, gate)
		}
	}
	return failed
}

// RenderSummary is the Markdown the report workflow pastes into the check
// run and the GitHub step summary.
func RenderSummary(evaluation Evaluation) string {
	var body strings.Builder
	status := "passed"
	if !evaluation.Passed {
		status = "failed"
	}
	fmt.Fprintf(&body, "# Soak %s (%s)\n\n", status, VerdictClass(evaluation))
	fmt.Fprintf(&body, "- samples: %d (%d steady)\n", evaluation.Samples, evaluation.SteadySamples)
	fmt.Fprintf(&body, "- window: warmup %s, steady %s\n", evaluation.WarmupWindow, evaluation.SteadyWindow)
	if evaluation.Enforced {
		body.WriteString("- enforce: on\n")
	} else {
		body.WriteString("- enforce: off (breaches recorded, run still passes)\n")
	}
	if evaluation.ThresholdsDigest != "" {
		fmt.Fprintf(&body, "- thresholds: %s\n", evaluation.ThresholdsDigest)
	}
	if evaluation.PackageLocator != "" {
		fmt.Fprintf(&body, "- package: %s\n", evaluation.PackageLocator)
	}
	if evaluation.QRLTests != "" {
		fmt.Fprintf(&body, "- qrl-tests: %s\n", evaluation.QRLTests)
	}
	body.WriteString("\n| Gate | Passed | Observed | Threshold |\n| --- | --- | --- | --- |\n")
	for _, gate := range evaluation.Gates {
		mark := "yes"
		if !gate.Passed {
			mark = "no"
		} else if gate.Insufficient {
			mark = "n/a"
		}
		fmt.Fprintf(&body, "| %s | %s | %s | %s |\n",
			escapeCell(gate.Name), mark, escapeCell(gate.Observed), escapeCell(gate.Threshold))
	}
	body.WriteByte('\n')
	return body.String()
}

// RenderComparedSummary is RenderSummary plus the week-over-week table
// soak-report writes after it downloads the previous artifact.
func RenderComparedSummary(evaluation Evaluation, comparison Comparison) string {
	var body strings.Builder
	body.WriteString(RenderSummary(evaluation))
	body.WriteString(RenderComparison(comparison))
	return body.String()
}

// RenderComparison is the Markdown section under the gate table.
func RenderComparison(comparison Comparison) string {
	var body strings.Builder
	body.WriteString("## Versus previous soak\n\n")
	if !comparison.Comparable {
		reason := comparison.Reason
		if reason == "" {
			reason = "no comparable baseline"
		}
		fmt.Fprintf(&body, "Skipped: %s.\n\n", reason)
		return body.String()
	}
	for _, note := range comparison.Notes {
		fmt.Fprintf(&body, "- %s\n", note)
	}
	if len(comparison.Notes) > 0 {
		body.WriteByte('\n')
	}
	body.WriteString("| Metric | This run | Previous | Change |\n| --- | --- | --- | --- |\n")
	for _, delta := range comparison.Deltas {
		change := delta.Change
		if delta.Worse {
			change += " worse"
		}
		fmt.Fprintf(&body, "| %s | %s | %s | %s |\n",
			escapeCell(delta.Name), escapeCell(delta.Current), escapeCell(delta.Baseline), escapeCell(change))
	}
	body.WriteByte('\n')
	return body.String()
}

func escapeCell(value string) string {
	return strings.ReplaceAll(value, "|", "\\|")
}
