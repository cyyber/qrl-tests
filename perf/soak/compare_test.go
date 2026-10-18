package soak

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/internal/jsonfile"
	"github.com/stretchr/testify/require"
)

func TestCompareHeadlineDeltas(t *testing.T) {
	current := comparableEvaluation()
	current.Metrics.MissedSlotRate = 0.05
	current.Metrics.CanaryP95 = 4 * time.Second
	current.Metrics.MemorySlopes["participant-1/execution/rss"] = MemoryTrend{SlopeMBPerHour: 12}

	baseline := comparableEvaluation()
	baseline.Metrics.MissedSlotRate = 0.04
	baseline.Metrics.CanaryP95 = 2 * time.Second
	baseline.Metrics.MemorySlopes["participant-1/execution/rss"] = MemoryTrend{SlopeMBPerHour: 10}

	comparison := Compare(current, baseline)
	require.True(t, comparison.Comparable)
	require.Equal(t, "passed", comparison.CurrentClass)
	require.Equal(t, "passed", comparison.BaselineClass)

	missed := delta(t, comparison, "missed-slot rate")
	require.Equal(t, "5.00%", missed.Current)
	require.Equal(t, "4.00%", missed.Baseline)
	require.Equal(t, "+25.0%", missed.Change)
	require.True(t, missed.Worse)

	p95 := delta(t, comparison, "canary p95")
	require.Equal(t, "4.00s", p95.Current)
	require.Equal(t, "+100.0%", p95.Change)
	require.True(t, p95.Worse)

	rss := delta(t, comparison, "memory/participant-1/execution/rss")
	require.Equal(t, "12.00 MB/h", rss.Current)
	require.Equal(t, "+20.0%", rss.Change)
	require.False(t, rss.Worse, "2 MB/h is under the MB/h noise floor")
}

func TestCompareAbsoluteDeltaBelowNoiseFloor(t *testing.T) {
	current := comparableEvaluation()
	current.Metrics.ProcessSlopes = map[string]MemoryTrend{
		"participant-1/validator/gc-pause": {SlopeMBPerHour: 83.3},
		"participant-1/validator/fds":      {SlopeMBPerHour: 12},
	}
	current.Metrics.GCPerHour = map[string]float64{"participant-1/validator/gc-rate": 900}
	current.Metrics.RPCErrorRate = 0.001
	baseline := comparableEvaluation()
	baseline.Metrics.ProcessSlopes = map[string]MemoryTrend{
		"participant-1/validator/gc-pause": {SlopeMBPerHour: 0.45},
		"participant-1/validator/fds":      {SlopeMBPerHour: 0},
	}
	baseline.Metrics.GCPerHour = map[string]float64{"participant-1/validator/gc-rate": 0}
	baseline.Metrics.RPCErrorRate = 0

	comparison := Compare(current, baseline)
	require.True(t, comparison.Comparable)

	pause := delta(t, comparison, "process/participant-1/validator/gc-pause")
	require.Equal(t, "83.30 ms/h", pause.Current)
	require.Equal(t, "0.45 ms/h", pause.Baseline)
	require.Equal(t, "+82.85 ms/h", pause.Change, "a percentage of a 0.45 ms/h baseline is meaningless")
	require.False(t, pause.Worse)

	fds := delta(t, comparison, "process/participant-1/validator/fds")
	require.Equal(t, "12.00 /h", fds.Current)
	require.Equal(t, "+12.00 /h", fds.Change)
	require.False(t, fds.Worse)

	gc := delta(t, comparison, "process/participant-1/validator/gc-rate")
	require.Equal(t, "900 GC/h", gc.Current)
	require.Equal(t, "+900 GC/h", gc.Change)
	require.False(t, gc.Worse)

	rpc := delta(t, comparison, "rpc error rate")
	require.Equal(t, "0.10%", rpc.Current)
	require.Equal(t, "+0.10 pp", rpc.Change)
	require.False(t, rpc.Worse)

	require.Equal(t, "no change", delta(t, comparison, "consensus split samples").Change)
}

func TestCompareWorseNeedsBandAndFloor(t *testing.T) {
	current := comparableEvaluation()
	current.Metrics.MemorySlopes = map[string]MemoryTrend{
		"participant-1/execution/rss":  {SlopeMBPerHour: 105},
		"participant-1/execution/heap": {SlopeMBPerHour: 120},
	}
	current.Metrics.MissedSlotRate = 0.0104
	current.Metrics.HeadBlocksPerMinute = map[int]float64{1: 10}
	baseline := comparableEvaluation()
	baseline.Metrics.MemorySlopes = map[string]MemoryTrend{
		"participant-1/execution/rss":  {SlopeMBPerHour: 100},
		"participant-1/execution/heap": {SlopeMBPerHour: 100},
	}
	baseline.Metrics.MissedSlotRate = 0.01
	baseline.Metrics.HeadBlocksPerMinute = map[int]float64{1: 12}

	comparison := Compare(current, baseline)
	rss := delta(t, comparison, "memory/participant-1/execution/rss")
	require.Equal(t, "+5.0%", rss.Change)
	require.False(t, rss.Worse, "within the 10% band")

	heap := delta(t, comparison, "memory/participant-1/execution/heap")
	require.Equal(t, "+20.0%", heap.Change)
	require.True(t, heap.Worse, "over the band and the 8 MB/h floor")

	missed := delta(t, comparison, "missed-slot rate")
	require.Equal(t, "+4.0%", missed.Change)
	require.False(t, missed.Worse)

	head := delta(t, comparison, "head blocks/min participant-1")
	require.Equal(t, "-16.7%", head.Change)
	require.True(t, head.Worse, "fewer blocks is the harmful direction")
}

func TestCompareShowsNotJudgedGates(t *testing.T) {
	current := comparableEvaluation()
	current.Gates = append(current.Gates,
		Gate{Name: "memory/participant-1/execution/rss", Passed: true, Insufficient: true, Observed: "window 17m0s over 35 samples"},
		Gate{Name: "process/participant-1/validator/gc-rate", Passed: true, Insufficient: true},
		Gate{Name: "memory/participant-1/execution/goroutines", Passed: true, Insufficient: true},
	)
	delete(current.Metrics.MemorySlopes, "participant-1/execution/rss")
	baseline := comparableEvaluation()
	baseline.Gates = append(baseline.Gates, Gate{Name: "chain-progress/missed-slots", Passed: true, Insufficient: true, Observed: "no consensus slot data"})
	baseline.Metrics.GCPerHour = map[string]float64{"participant-1/validator/gc-rate": 1200}

	comparison := Compare(current, baseline)
	require.True(t, comparison.Comparable)

	rss := delta(t, comparison, "memory/participant-1/execution/rss")
	require.Equal(t, NotJudged, rss.Current)
	require.Equal(t, "8.00 MB/h", rss.Baseline)
	require.Equal(t, NotJudged, rss.Change)
	require.False(t, rss.Worse)

	gc := delta(t, comparison, "process/participant-1/validator/gc-rate")
	require.Equal(t, NotJudged, gc.Current)
	require.Equal(t, "1200 GC/h", gc.Baseline)
	require.Equal(t, NotJudged, gc.Change)

	missed := delta(t, comparison, "missed-slot rate")
	require.Equal(t, "1.00%", missed.Current)
	require.Equal(t, NotJudged, missed.Baseline)
	require.Equal(t, NotJudged, missed.Change)

	for _, item := range comparison.Deltas {
		require.NotContains(t, item.Name, "goroutines", "goroutine slopes are gated, not compared")
	}
	require.Contains(t, RenderComparison(comparison), "| memory/participant-1/execution/rss | n/a | 8.00 MB/h | n/a |")
}

func TestCompareWindowNoteIgnoresJitter(t *testing.T) {
	current := comparableEvaluation()
	current.SteadyWindow = 17*time.Minute - 10*time.Millisecond
	baseline := comparableEvaluation()
	baseline.SteadyWindow = 17*time.Minute + 8*time.Millisecond

	comparison := Compare(current, baseline)
	require.True(t, comparison.Comparable)
	require.Empty(t, comparison.Notes)

	current.SteadyWindow = 4 * time.Hour
	baseline.SteadyWindow = 4*time.Hour - 3*time.Minute
	require.Empty(t, Compare(current, baseline).Notes, "3 minutes of a 4 hour window is under 5%")

	baseline.SteadyWindow = 3*time.Hour + 400*time.Millisecond
	require.Equal(t, []string{"steady windows differ (4h0m0s vs 3h0m0s)"}, Compare(current, baseline).Notes)
}

func TestCompareRefusesThresholdsChange(t *testing.T) {
	current := comparableEvaluation()
	current.ThresholdsDigest = "sha256:aaa"
	baseline := comparableEvaluation()
	baseline.ThresholdsDigest = "sha256:bbb"

	comparison := Compare(current, baseline)
	require.False(t, comparison.Comparable)
	require.Contains(t, comparison.Reason, ReasonThresholdsChanged)
	require.Empty(t, comparison.Deltas)
}

func TestCompareRefusesInfrastructure(t *testing.T) {
	current := comparableEvaluation()
	baseline := comparableEvaluation()
	baseline.Gates = append(baseline.Gates, Gate{Name: "placement/pinned", Passed: false})

	comparison := Compare(current, baseline)
	require.False(t, comparison.Comparable)
	require.Equal(t, ReasonBaselineInfrastructure, comparison.Reason)

	comparison = Compare(baseline, current)
	require.False(t, comparison.Comparable)
	require.Equal(t, ReasonCurrentInfrastructure, comparison.Reason)
}

func TestCompareNotesProvenanceDrift(t *testing.T) {
	current := comparableEvaluation()
	current.QRLTests = "aaa"
	current.PackageLocator = "github.com/cyyber/qrl-package@111"
	current.Images = map[string]string{"execution": "ghcr.io/a@sha256:1"}
	baseline := comparableEvaluation()
	baseline.QRLTests = "bbb"
	baseline.PackageLocator = "github.com/cyyber/qrl-package@222"
	baseline.Images = map[string]string{"execution": "ghcr.io/a@sha256:2"}

	comparison := Compare(current, baseline)
	require.True(t, comparison.Comparable)
	require.Contains(t, comparison.Notes, "qrl-tests revision differs")
	require.Contains(t, comparison.Notes, "qrl-package pin differs")
	require.Contains(t, comparison.Notes, "image digests differ")
}

func TestCompareNotesMissingDigestAndWindow(t *testing.T) {
	current := comparableEvaluation()
	current.ThresholdsDigest = "sha256:aaa"
	current.SteadyWindow = 4 * time.Hour
	baseline := comparableEvaluation()
	baseline.ThresholdsDigest = ""
	baseline.SteadyWindow = 15 * time.Minute

	comparison := Compare(current, baseline)
	require.True(t, comparison.Comparable)
	require.Contains(t, comparison.Notes, "thresholds digest missing on one side; comparing metrics anyway")
	require.Contains(t, comparison.Notes[1], "steady windows differ")
}

func TestCompareSkipsCanaryWhenOnlyOneSideSent(t *testing.T) {
	current := comparableEvaluation()
	current.Metrics.CanarySent = 0
	current.Metrics.CanaryP95 = 0
	baseline := comparableEvaluation()

	comparison := Compare(current, baseline)
	require.True(t, comparison.Comparable)
	require.Contains(t, comparison.Notes, "canary metrics skipped; only one run sent canaries")
	for _, item := range comparison.Deltas {
		require.NotContains(t, item.Name, "canary")
	}
}

func TestWriteComparisonRewritesSummary(t *testing.T) {
	dir := t.TempDir()
	current := comparableEvaluation()
	current.Metrics.MissedSlotRate = 0.02
	baseline := comparableEvaluation()
	baseline.Metrics.MissedSlotRate = 0.01
	currentPath := filepath.Join(dir, "current.json")
	baselinePath := filepath.Join(dir, "baseline.json")
	require.NoError(t, jsonfile.Write(currentPath, current, "current"))
	require.NoError(t, jsonfile.Write(baselinePath, baseline, "baseline"))

	summaryPath := filepath.Join(dir, SummaryFile)
	outputPath := filepath.Join(dir, ComparisonFile)
	comparison, err := WriteComparison(currentPath, baselinePath, summaryPath, outputPath)
	require.NoError(t, err)
	require.True(t, comparison.Comparable)

	markdown, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	require.Contains(t, string(markdown), "# Soak passed")
	require.Contains(t, string(markdown), "## Versus previous soak")
	require.Contains(t, string(markdown), "missed-slot rate")
	require.Contains(t, string(markdown), "+100.0% worse")

	written, err := jsonfile.Read[Comparison](outputPath, "comparison")
	require.NoError(t, err)
	require.True(t, written.Comparable)
}

func TestRenderComparisonSkipped(t *testing.T) {
	markdown := RenderComparison(Comparison{Reason: ReasonBaselineInfrastructure})
	require.Contains(t, markdown, "## Versus previous soak")
	require.Contains(t, markdown, "Skipped: previous run was infrastructure.")
}

func comparableEvaluation() Evaluation {
	return Evaluation{
		Passed:           true,
		Enforced:         false,
		Samples:          10,
		SteadySamples:    8,
		SteadyWindow:     time.Hour,
		ThresholdsDigest: "sha256:same",
		Gates:            []Gate{{Name: "chain-progress/head", Passed: true, Observed: "12.00 blocks/min"}},
		Metrics: Metrics{
			MissedSlotRate:      0.01,
			RPCErrorRate:        0,
			MaxFinalityLag:      1,
			HeadBlocksPerMinute: map[int]float64{1: 12},
			CanarySent:          8,
			CanaryP50:           time.Second,
			CanaryP95:           2 * time.Second,
			MemorySlopes: map[string]MemoryTrend{
				"participant-1/execution/rss": {SlopeMBPerHour: 8},
			},
		},
	}
}

func delta(t *testing.T, comparison Comparison, name string) Delta {
	t.Helper()
	for _, item := range comparison.Deltas {
		if item.Name == name {
			return item
		}
	}
	t.Fatalf("delta %q not found in %#v", name, comparison.Deltas)
	return Delta{}
}
