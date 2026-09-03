package soak

import (
	"fmt"
	"maps"
	"math"
	"os"
	"slices"
	"time"

	"github.com/cyyber/qrl-tests/internal/jsonfile"
)

const (
	// worseDeltaThreshold marks a delta as worse in the summary. It does
	// not fail the run; first weeks are calibration.
	worseDeltaThreshold = 0.20

	ReasonThresholdsChanged      = "thresholds digest differs"
	ReasonBaselineInfrastructure = "previous run was infrastructure"
	ReasonCurrentInfrastructure  = "current run is infrastructure"
	ReasonNoComparableMetrics    = "no overlapping metrics to compare"
)

// Comparison is this soak against a previous results.json.
type Comparison struct {
	Comparable    bool     `json:"comparable"`
	Reason        string   `json:"reason,omitempty"`
	Notes         []string `json:"notes,omitempty"`
	CurrentClass  string   `json:"current_class"`
	BaselineClass string   `json:"baseline_class"`
	Deltas        []Delta  `json:"deltas,omitempty"`
}

// Delta is one headline number versus the previous soak.
type Delta struct {
	Name     string `json:"name"`
	Current  string `json:"current"`
	Baseline string `json:"baseline"`
	Change   string `json:"change"`
	Worse    bool   `json:"worse,omitempty"`
}

// Compare diffs headline metrics. It refuses when the thresholds file
// changed or either run is an infrastructure failure.
func Compare(current, baseline Evaluation) Comparison {
	comparison := Comparison{
		CurrentClass:  VerdictClass(current),
		BaselineClass: VerdictClass(baseline),
	}
	if comparison.CurrentClass == "infrastructure" {
		comparison.Reason = ReasonCurrentInfrastructure
		return comparison
	}
	if comparison.BaselineClass == "infrastructure" {
		comparison.Reason = ReasonBaselineInfrastructure
		return comparison
	}
	if current.ThresholdsDigest != "" && baseline.ThresholdsDigest != "" &&
		current.ThresholdsDigest != baseline.ThresholdsDigest {
		comparison.Reason = fmt.Sprintf("%s (%s vs %s)",
			ReasonThresholdsChanged, current.ThresholdsDigest, baseline.ThresholdsDigest)
		return comparison
	}
	if current.ThresholdsDigest != baseline.ThresholdsDigest {
		comparison.Notes = append(comparison.Notes, "thresholds digest missing on one side; comparing metrics anyway")
	}
	if current.QRLTests != "" && baseline.QRLTests != "" && current.QRLTests != baseline.QRLTests {
		comparison.Notes = append(comparison.Notes, "qrl-tests revision differs")
	}
	if current.PackageLocator != "" && baseline.PackageLocator != "" && current.PackageLocator != baseline.PackageLocator {
		comparison.Notes = append(comparison.Notes, "qrl-package pin differs")
	}
	if len(current.Images) > 0 && len(baseline.Images) > 0 && !maps.Equal(current.Images, baseline.Images) {
		comparison.Notes = append(comparison.Notes, "image digests differ")
	}
	if current.SteadyWindow > 0 && baseline.SteadyWindow > 0 && current.SteadyWindow != baseline.SteadyWindow {
		comparison.Notes = append(comparison.Notes, fmt.Sprintf("steady windows differ (%s vs %s)",
			current.SteadyWindow, baseline.SteadyWindow))
	}

	addRate(&comparison, "missed-slot rate", current.Metrics.MissedSlotRate, baseline.Metrics.MissedSlotRate, true)
	addRate(&comparison, "rpc error rate", current.Metrics.RPCErrorRate, baseline.Metrics.RPCErrorRate, true)
	addCount(&comparison, "finality lag (epochs)", float64(current.Metrics.MaxFinalityLag), float64(baseline.Metrics.MaxFinalityLag), "epochs", true)
	addCount(&comparison, "consensus split samples", float64(current.Metrics.SplitSamples), float64(baseline.Metrics.SplitSamples), "samples", true)

	for _, id := range slices.Sorted(maps.Keys(current.Metrics.HeadBlocksPerMinute)) {
		baselineRate, ok := baseline.Metrics.HeadBlocksPerMinute[id]
		if !ok {
			continue
		}
		addFloat(&comparison, fmt.Sprintf("head blocks/min participant-%d", id),
			current.Metrics.HeadBlocksPerMinute[id], baselineRate, "%.2f blocks/min", false)
	}

	if current.Metrics.CanarySent > 0 && baseline.Metrics.CanarySent > 0 {
		addDuration(&comparison, "canary p50", current.Metrics.CanaryP50, baseline.Metrics.CanaryP50, true)
		addDuration(&comparison, "canary p95", current.Metrics.CanaryP95, baseline.Metrics.CanaryP95, true)
		addRate(&comparison, "canary failure rate", current.Metrics.CanaryFailureRate, baseline.Metrics.CanaryFailureRate, true)
	} else if current.Metrics.CanarySent > 0 || baseline.Metrics.CanarySent > 0 {
		comparison.Notes = append(comparison.Notes, "canary metrics skipped; only one run sent canaries")
	}

	addTrends(&comparison, "rss", current.Metrics.MemorySlopes, baseline.Metrics.MemorySlopes)
	addTrends(&comparison, "process", current.Metrics.ProcessSlopes, baseline.Metrics.ProcessSlopes)
	for _, key := range slices.Sorted(maps.Keys(current.Metrics.GCPerHour)) {
		baselineRate, ok := baseline.Metrics.GCPerHour[key]
		if !ok {
			continue
		}
		addFloat(&comparison, "gc-rate/"+key, current.Metrics.GCPerHour[key], baselineRate, "%.0f GC/h", true)
	}
	addTrends(&comparison, "working-set", current.Metrics.WorkingSetSlopes, baseline.Metrics.WorkingSetSlopes)
	for _, key := range slices.Sorted(maps.Keys(current.Metrics.PeakWorkingSetShare)) {
		baselineShare, ok := baseline.Metrics.PeakWorkingSetShare[key]
		if !ok {
			continue
		}
		addRate(&comparison, "working-set share/"+key, current.Metrics.PeakWorkingSetShare[key], baselineShare, true)
	}

	if len(comparison.Deltas) == 0 {
		comparison.Reason = ReasonNoComparableMetrics
		return comparison
	}
	comparison.Comparable = true
	return comparison
}

// WriteComparison reads two results.json files, writes comparison.json,
// and rewrites summary.md with the week-over-week table.
func WriteComparison(currentPath, baselinePath, summaryPath, outputPath string) (Comparison, error) {
	current, err := jsonfile.Read[Evaluation](currentPath, "current soak results")
	if err != nil {
		return Comparison{}, err
	}
	baseline, err := jsonfile.Read[Evaluation](baselinePath, "baseline soak results")
	if err != nil {
		return Comparison{}, err
	}
	comparison := Compare(current, baseline)
	if outputPath != "" {
		if err := jsonfile.Write(outputPath, comparison, "soak comparison"); err != nil {
			return Comparison{}, err
		}
	}
	if summaryPath != "" {
		if err := os.WriteFile(summaryPath, []byte(RenderComparedSummary(current, comparison)), 0o600); err != nil {
			return Comparison{}, fmt.Errorf("write soak summary: %w", err)
		}
	}
	return comparison, nil
}

func addTrends(comparison *Comparison, kind string, current, baseline map[string]MemoryTrend) {
	for _, key := range slices.Sorted(maps.Keys(current)) {
		previous, ok := baseline[key]
		if !ok {
			continue
		}
		addFloat(comparison, kind+"/"+key, current[key].SlopeMBPerHour, previous.SlopeMBPerHour, "%.2f MB/h", true)
	}
}

func addRate(comparison *Comparison, name string, current, baseline float64, higherIsWorse bool) {
	change, worse := classifyDelta(current, baseline, higherIsWorse)
	comparison.Deltas = append(comparison.Deltas, Delta{
		Name:     name,
		Current:  formatRate(current),
		Baseline: formatRate(baseline),
		Change:   change,
		Worse:    worse,
	})
}

func addDuration(comparison *Comparison, name string, current, baseline time.Duration, higherIsWorse bool) {
	change, worse := classifyDelta(current.Seconds(), baseline.Seconds(), higherIsWorse)
	comparison.Deltas = append(comparison.Deltas, Delta{
		Name:     name,
		Current:  current.String(),
		Baseline: baseline.String(),
		Change:   change,
		Worse:    worse,
	})
}

func addCount(comparison *Comparison, name string, current, baseline float64, unit string, higherIsWorse bool) {
	change, worse := classifyDelta(current, baseline, higherIsWorse)
	comparison.Deltas = append(comparison.Deltas, Delta{
		Name:     name,
		Current:  fmt.Sprintf("%.0f %s", current, unit),
		Baseline: fmt.Sprintf("%.0f %s", baseline, unit),
		Change:   change,
		Worse:    worse,
	})
}

func addFloat(comparison *Comparison, name string, current, baseline float64, format string, higherIsWorse bool) {
	change, worse := classifyDelta(current, baseline, higherIsWorse)
	comparison.Deltas = append(comparison.Deltas, Delta{
		Name:     name,
		Current:  fmt.Sprintf(format, current),
		Baseline: fmt.Sprintf(format, baseline),
		Change:   change,
		Worse:    worse,
	})
}

func classifyDelta(current, baseline float64, higherIsWorse bool) (string, bool) {
	if baseline == 0 {
		if current == 0 {
			return "0%", false
		}
		return "n/a", (current > baseline) == higherIsWorse
	}
	rel := (current - baseline) / math.Abs(baseline)
	worse := (rel > worseDeltaThreshold && higherIsWorse) || (rel < -worseDeltaThreshold && !higherIsWorse)
	return fmt.Sprintf("%+.1f%%", rel*100), worse
}

func formatRate(rate float64) string {
	return fmt.Sprintf("%.2f%%", rate*100)
}
