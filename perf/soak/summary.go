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

func escapeCell(value string) string {
	return strings.ReplaceAll(value, "|", "\\|")
}
