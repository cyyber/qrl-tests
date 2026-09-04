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
	require.Equal(t, "4s", p95.Current)
	require.True(t, p95.Worse)

	rss := delta(t, comparison, "rss/participant-1/execution/rss")
	require.Equal(t, "12.00 MB/h", rss.Current)
	require.False(t, rss.Worse, "20% exactly is not worse")
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
