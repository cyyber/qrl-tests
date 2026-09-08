package soak

import (
	"fmt"
	"maps"
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/internal/jsonfile"
)

const (
	// worseRelativeBand is the relative change a delta must exceed, on top
	// of its unit's absolute floor, before the summary labels it worse.
	// Labels never fail the run; first weeks are calibration.
	worseRelativeBand = 0.10

	// Steady windows are compared to whole seconds and only noted when they
	// differ by more than the larger of windowDriftFloor and
	// windowDriftShare of the longer window; a 10 ms sampling jitter is not
	// a different run.
	windowDriftShare = 0.05
	windowDriftFloor = 30 * time.Second

	ReasonThresholdsChanged      = "thresholds digest differs"
	ReasonBaselineInfrastructure = "previous run was infrastructure"
	ReasonCurrentInfrastructure  = "current run is infrastructure"
	ReasonNoComparableMetrics    = "no overlapping metrics to compare"

	// NotJudged is the cell for a metric whose gate was n/a in that run.
	NotJudged = "n/a"
)

// unit is how a headline number prints and how much of a difference is
// noise for it. value and delta are Printf verbs applied to the stored
// number times scale; floor is in stored units.
type unit struct {
	value string
	delta string
	scale float64
	floor float64
}

func (u unit) formatValue(value float64) string { return fmt.Sprintf(u.value, value*u.scale) }
func (u unit) formatDelta(delta float64) string { return fmt.Sprintf(u.delta, delta*u.scale) }

// Noise floors: a baseline below the floor has no meaningful percentage
// (a 0.45 ms/h GC-pause slope against 83 read as +18298%), and a
// difference below it is never worse. Rates are in percentage points,
// durations in seconds, slopes in their own per-hour unit.
var (
	unitRate      = unit{value: "%.2f%%", delta: "%+.2f pp", scale: 100, floor: 0.005}
	unitSeconds   = unit{value: "%.2fs", delta: "%+.2fs", scale: 1, floor: 1}
	unitEpochs    = unit{value: "%.0f epochs", delta: "%+.0f epochs", scale: 1, floor: 1}
	unitSamples   = unit{value: "%.0f samples", delta: "%+.0f samples", scale: 1, floor: 1}
	unitBlocksMin = unit{value: "%.2f blocks/min", delta: "%+.2f blocks/min", scale: 1, floor: 0.5}
	unitMBPerHour = unit{value: "%.2f MB/h", delta: "%+.2f MB/h", scale: 1, floor: 8}
	unitFDPerHour = unit{value: "%.2f /h", delta: "%+.2f /h", scale: 1, floor: 10}
	unitMSPerHour = unit{value: "%.2f ms/h", delta: "%+.2f ms/h", scale: 1, floor: 5}
	unitGCPerHour = unit{value: "%.0f GC/h", delta: "%+.0f GC/h", scale: 1, floor: 600}
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

// Delta is one headline number versus the previous soak. Change is a
// relative percentage when the baseline is above the metric's noise floor,
// the absolute difference in the metric's unit when it is not, and n/a
// when either run's gate was not judged.
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
	if note, differ := windowsDiffer(current.SteadyWindow, baseline.SteadyWindow); differ {
		comparison.Notes = append(comparison.Notes, note)
	}

	c := comparer{
		comparison: &comparison,
		current:    current.Metrics,
		baseline:   baseline.Metrics,
		currentNA:  notJudged(current),
		baselineNA: notJudged(baseline),
	}
	c.add("missed-slot rate", "chain-progress/missed-slots", c.current.MissedSlotRate, c.baseline.MissedSlotRate, unitRate, true)
	c.add("rpc error rate", "rpc/error-rate", c.current.RPCErrorRate, c.baseline.RPCErrorRate, unitRate, true)
	c.add("finality lag (epochs)", "finality/lag", float64(c.current.MaxFinalityLag), float64(c.baseline.MaxFinalityLag), unitEpochs, true)
	c.add("consensus split samples", "consensus/split", float64(c.current.SplitSamples), float64(c.baseline.SplitSamples), unitSamples, true)

	for _, id := range slices.Sorted(maps.Keys(c.current.HeadBlocksPerMinute)) {
		baselineRate, ok := c.baseline.HeadBlocksPerMinute[id]
		if !ok {
			continue
		}
		c.add(fmt.Sprintf("head blocks/min participant-%d", id), fmt.Sprintf("chain-progress/participant-%d", id),
			c.current.HeadBlocksPerMinute[id], baselineRate, unitBlocksMin, false)
	}

	if c.current.CanarySent > 0 && c.baseline.CanarySent > 0 {
		c.add("canary p50", "canary/latency", c.current.CanaryP50.Seconds(), c.baseline.CanaryP50.Seconds(), unitSeconds, true)
		c.add("canary p95", "canary/latency", c.current.CanaryP95.Seconds(), c.baseline.CanaryP95.Seconds(), unitSeconds, true)
		c.add("canary failure rate", "canary/failures", c.current.CanaryFailureRate, c.baseline.CanaryFailureRate, unitRate, true)
	} else if c.current.CanarySent > 0 || c.baseline.CanarySent > 0 {
		comparison.Notes = append(comparison.Notes, "canary metrics skipped; only one run sent canaries")
	}

	c.addTrends("memory/", c.current.MemorySlopes, c.baseline.MemorySlopes, memoryUnit)
	c.addTrends("process/", c.current.ProcessSlopes, c.baseline.ProcessSlopes, processUnit)
	c.addRates("process/", c.current.GCPerHour, c.baseline.GCPerHour, gcRateUnit)
	c.addTrends("working-set/", c.current.WorkingSetSlopes, c.baseline.WorkingSetSlopes, workingSetUnit)
	for _, key := range slices.Sorted(maps.Keys(c.current.PeakWorkingSetShare)) {
		baselineShare, ok := c.baseline.PeakWorkingSetShare[key]
		if !ok {
			continue
		}
		c.add("working-set share/"+key, "working-set/"+key+"/headroom", c.current.PeakWorkingSetShare[key], baselineShare, unitRate, true)
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

// comparer adds deltas, showing n/a for any metric whose gate was not
// judged (Insufficient) in either run instead of comparing numbers that
// were never computed.
type comparer struct {
	comparison            *Comparison
	current, baseline     Metrics
	currentNA, baselineNA map[string]bool
}

func notJudged(evaluation Evaluation) map[string]bool {
	gates := make(map[string]bool)
	for _, gate := range evaluation.Gates {
		if gate.Insufficient {
			gates[gate.Name] = true
		}
	}
	return gates
}

func (c *comparer) add(name, gate string, current, baseline float64, u unit, higherIsWorse bool) {
	if c.currentNA[gate] || c.baselineNA[gate] {
		c.comparison.Deltas = append(c.comparison.Deltas, Delta{
			Name:     name,
			Current:  c.cell(current, c.currentNA[gate], u),
			Baseline: c.cell(baseline, c.baselineNA[gate], u),
			Change:   NotJudged,
		})
		return
	}

	change, worse := classifyDelta(current, baseline, u, higherIsWorse)
	c.comparison.Deltas = append(c.comparison.Deltas, Delta{
		Name:     name,
		Current:  u.formatValue(current),
		Baseline: u.formatValue(baseline),
		Change:   change,
		Worse:    worse,
	})
}

func (c *comparer) cell(value float64, notJudged bool, u unit) string {
	if notJudged {
		return NotJudged
	}
	return u.formatValue(value)
}

// addTrends walks the union of both runs' slopes and the not-judged gates
// under prefix, so a slope that was n/a on one side still gets a row. Gate
// names double as delta names. unitFor returns false for keys under the
// prefix that are not slopes of this kind (goroutines, headroom, gc-rate).
func (c *comparer) addTrends(prefix string, current, baseline map[string]MemoryTrend, unitFor func(key string) (unit, bool)) {
	for _, key := range keys(c, prefix, current, baseline) {
		u, ok := unitFor(key)
		if !ok {
			continue
		}

		gate := prefix + key
		now, nowOK := current[key]
		previous, previousOK := baseline[key]
		if !c.notJudged(gate) && (!nowOK || !previousOK) {
			continue
		}
		c.add(gate, gate, now.SlopeMBPerHour, previous.SlopeMBPerHour, u, true)
	}
}

func (c *comparer) addRates(prefix string, current, baseline map[string]float64, unitFor func(key string) (unit, bool)) {
	for _, key := range keys(c, prefix, current, baseline) {
		u, ok := unitFor(key)
		if !ok {
			continue
		}

		gate := prefix + key
		now, nowOK := current[key]
		previous, previousOK := baseline[key]
		if !c.notJudged(gate) && (!nowOK || !previousOK) {
			continue
		}
		c.add(gate, gate, now, previous, u, true)
	}
}

func (c *comparer) notJudged(gate string) bool {
	return c.currentNA[gate] || c.baselineNA[gate]
}

// keys is the sorted union of both runs' metric keys and the not-judged
// gates under prefix, stripped of it.
func keys[V any](c *comparer, prefix string, current, baseline map[string]V) []string {
	union := make(map[string]bool)
	for key := range current {
		union[key] = true
	}
	for key := range baseline {
		union[key] = true
	}
	for _, gates := range []map[string]bool{c.currentNA, c.baselineNA} {
		for gate := range gates {
			if strings.HasPrefix(gate, prefix) {
				union[strings.TrimPrefix(gate, prefix)] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(union))
}

func memoryUnit(key string) (unit, bool) {
	return unitMBPerHour, strings.HasSuffix(key, "/rss") || strings.HasSuffix(key, "/heap")
}

func processUnit(key string) (unit, bool) {
	switch {
	case strings.HasSuffix(key, "/fds"):
		return unitFDPerHour, true
	case strings.HasSuffix(key, "/gc-pause"):
		return unitMSPerHour, true
	}
	return unit{}, false
}

func gcRateUnit(key string) (unit, bool) {
	return unitGCPerHour, strings.HasSuffix(key, "/gc-rate")
}

func workingSetUnit(key string) (unit, bool) {
	return unitMBPerHour, !strings.HasSuffix(key, "/headroom")
}

// classifyDelta describes the change and whether it is worse. Below the
// unit's noise floor the baseline has no meaningful percentage, so the
// absolute difference is reported and never labelled worse; above it, a
// change is worse only when it exceeds both the floor and
// worseRelativeBand in the harmful direction.
func classifyDelta(current, baseline float64, u unit, higherIsWorse bool) (string, bool) {
	diff := current - baseline
	if math.Abs(baseline) < u.floor {
		if diff == 0 {
			return "no change", false
		}
		return u.formatDelta(diff), false
	}

	rel := diff / math.Abs(baseline)
	change := fmt.Sprintf("%+.1f%%", rel*100)
	if math.Abs(diff) <= u.floor || math.Abs(rel) <= worseRelativeBand {
		return change, false
	}
	return change, (diff > 0) == higherIsWorse
}

func windowsDiffer(current, baseline time.Duration) (string, bool) {
	current, baseline = current.Round(time.Second), baseline.Round(time.Second)
	if current <= 0 || baseline <= 0 {
		return "", false
	}

	drift := (current - baseline).Abs()
	tolerance := max(windowDriftFloor, time.Duration(windowDriftShare*float64(max(current, baseline))))
	if drift <= tolerance {
		return "", false
	}
	return fmt.Sprintf("steady windows differ (%s vs %s)", current, baseline), true
}
