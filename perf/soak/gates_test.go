package soak

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEvaluateChainAndMemory(t *testing.T) {
	thresholds := DefaultThresholds()
	thresholds.Memory.MinSamples = 2
	start := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	samples := []Sample{
		steady(start, 100, 100, 10, 1<<30),
		steady(start.Add(30*time.Minute), 460, 460, 10, 1<<30+10<<20),
	}

	evaluation := Evaluate(samples, thresholds, Options{Participants: 2, SlotsPerEpoch: 8, Enforce: true})
	require.True(t, evaluation.Passed, gatesDetail(evaluation))
	require.Equal(t, thresholds.Digest, evaluation.ThresholdsDigest)
	require.InDelta(t, 12.0, evaluation.Metrics.HeadBlocksPerMinute[1], 0.1)
	require.Contains(t, names(evaluation), "chain-progress/participant-1")
	require.Contains(t, names(evaluation), "memory/participant-1/execution/rss")
}

func TestEvaluateFailsOnStallAndSplit(t *testing.T) {
	thresholds := DefaultThresholds()
	thresholds.Memory.MinSamples = 2
	thresholds.Chain.MaxStalledSamples = 2
	start := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	samples := []Sample{
		steady(start, 100, 100, 10, 1<<30),
		steady(start.Add(time.Minute), 100, 100, 10, 1<<30),
		steady(start.Add(2*time.Minute), 100, 100, 10, 1<<30),
	}
	samples[2].Participants[1].ReferenceHash = "0xdead"
	samples[2].Participants[1].ReferenceState = "0xbeef"

	evaluation := Evaluate(samples, thresholds, Options{Participants: 2, SlotsPerEpoch: 8, Enforce: true})
	require.False(t, evaluation.Passed)
	require.False(t, gate(evaluation, "chain-progress/participant-1").Passed)
	require.False(t, gate(evaluation, "consensus/split").Passed)

	unenforced := Evaluate(samples, thresholds, Options{Participants: 2, SlotsPerEpoch: 8, Enforce: false})
	require.True(t, unenforced.Passed, "calibration mode records breaches but still passes")
	require.False(t, gate(unenforced, "consensus/split").Passed)
}

func TestEvaluateInsufficientSteadyState(t *testing.T) {
	evaluation := Evaluate([]Sample{{Phase: PhaseWarmup}}, DefaultThresholds(), Options{Enforce: true})
	require.False(t, evaluation.Passed)
	require.Equal(t, "steady-state", evaluation.Gates[0].Name)
}

func TestEvaluateCanary(t *testing.T) {
	thresholds := DefaultThresholds()
	thresholds.Memory.MinSamples = 99
	start := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	samples := []Sample{
		steady(start, 100, 100, 10, 1<<30),
		steady(start.Add(time.Minute), 112, 112, 10, 1<<30),
	}
	samples[0].Canary = &Canary{Included: true, Latency: 2 * time.Second}
	samples[1].Canary = &Canary{Included: true, Latency: 3 * time.Second}

	evaluation := Evaluate(samples, thresholds, Options{Participants: 2, SlotsPerEpoch: 8, Enforce: true})
	require.True(t, evaluation.Passed, gatesDetail(evaluation))
	require.Equal(t, 2*time.Second, evaluation.Metrics.CanaryP50)
	require.Equal(t, 3*time.Second, evaluation.Metrics.CanaryP95)
}

func TestEvaluatePlacement(t *testing.T) {
	thresholds := DefaultThresholds()
	thresholds.Memory.MinSamples = 99
	start := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	samples := []Sample{
		steady(start, 100, 100, 10, 1<<30),
		steady(start.Add(time.Minute), 112, 112, 10, 1<<30),
	}

	evaluation := Evaluate(samples, thresholds, Options{
		Participants: 2, SlotsPerEpoch: 8, Enforce: true,
		Placement: []Placement{{Participant: 1, Node: "ip-1", Pinned: true}, {Participant: 2, Node: "ip-2", Pinned: false}},
	})
	require.False(t, evaluation.Passed)
	require.Contains(t, gate(evaluation, "placement/one-participant-per-node").Detail, "2→ip-2")
}

func TestEvaluateRSSInsufficientBelowMinWindow(t *testing.T) {
	thresholds := DefaultThresholds()
	thresholds.Memory.MinSamples = 2
	start := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	// 15 minutes of a 541 MB/h-class climb: enough samples, too short to judge.
	samples := []Sample{
		steady(start, 100, 100, 10, 1<<30),
		steady(start.Add(15*time.Minute), 280, 280, 10, 1<<30+135<<20),
	}

	evaluation := Evaluate(samples, thresholds, Options{Participants: 2, SlotsPerEpoch: 8, Enforce: true})
	require.True(t, evaluation.Passed, gatesDetail(evaluation))
	rss := gate(evaluation, "memory/participant-1/consensus/rss")
	require.True(t, rss.Insufficient)
	require.True(t, rss.Passed)
	require.Contains(t, rss.Detail, "1h0m0s")
	require.True(t, gate(evaluation, "memory/participant-1/consensus/heap").Insufficient)
	require.NotContains(t, evaluation.Metrics.MemorySlopes, "participant-1/consensus/rss")
}

func TestEvaluateRSSJudgedAfterMinWindow(t *testing.T) {
	thresholds := DefaultThresholds()
	thresholds.Memory.MinSamples = 2
	start := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	samples := []Sample{
		steady(start, 100, 100, 10, 1<<30),
		steady(start.Add(time.Hour), 820, 820, 10, 1<<30+600<<20),
	}

	evaluation := Evaluate(samples, thresholds, Options{Participants: 2, SlotsPerEpoch: 8, Enforce: true})
	require.False(t, evaluation.Passed)
	rss := gate(evaluation, "memory/participant-1/consensus/rss")
	require.False(t, rss.Insufficient)
	require.False(t, rss.Passed)
}

func TestEvaluateProcessFDsAndGC(t *testing.T) {
	thresholds := DefaultThresholds()
	thresholds.Memory.MinSamples = 2
	thresholds.Memory.OpenFDSlopeMaxPerHour = 10
	start := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	samples := processSamples(start, time.Hour)

	evaluation := Evaluate(samples, thresholds, Options{Participants: 2, SlotsPerEpoch: 8, Enforce: true})
	fds := gate(evaluation, "process/participant-1/execution/fds")
	require.False(t, fds.Passed)
	require.False(t, fds.Insufficient)
	require.True(t, gate(evaluation, "process/participant-1/execution/gc-pause").Passed, gatesDetail(evaluation))
	require.True(t, gate(evaluation, "process/participant-1/execution/gc-rate").Passed, gatesDetail(evaluation))
	require.InDelta(t, 500, evaluation.Metrics.GCPerHour["participant-1/execution/gc-rate"], 1)
	require.Contains(t, evaluation.Metrics.ProcessSlopes, "participant-1/execution/fds")
	require.Contains(t, evaluation.Metrics.ProcessSlopes, "participant-1/execution/gc-pause")
	require.Contains(t, evaluation.Metrics.WorkingSetSlopes, "participant-1/execution")
	require.False(t, gate(evaluation, "working-set/participant-1/execution").Insufficient)
}

func TestEvaluateTrendsInsufficientBelowMinWindow(t *testing.T) {
	thresholds := DefaultThresholds()
	thresholds.Memory.MinSamples = 2
	thresholds.Memory.OpenFDSlopeMaxPerHour = 10
	thresholds.Memory.GCPauseSlopeMaxMSPerHour = 1
	thresholds.Memory.WorkingSetSlopeMaxMBPerHour = 1
	start := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	// 17 minutes of growth that would breach every slope once extrapolated
	// to an hour (18 ms of GC pause reads as 63 ms/h) is not a verdict.
	samples := processSamples(start, 17*time.Minute)

	evaluation := Evaluate(samples, thresholds, Options{Participants: 2, SlotsPerEpoch: 8, Enforce: true})
	require.True(t, evaluation.Passed, gatesDetail(evaluation))
	for _, name := range []string{
		"process/participant-1/execution/fds",
		"process/participant-1/execution/gc-pause",
		"process/participant-1/execution/gc-rate",
		"working-set/participant-1/execution",
		"memory/participant-1/execution/rss",
	} {
		judged := gate(evaluation, name)
		require.True(t, judged.Passed, name)
		require.True(t, judged.Insufficient, name)
		require.Contains(t, judged.Observed, "window 17m0s over 2 samples", name)
		require.Contains(t, judged.Detail, "1h0m0s", name)
	}
	require.Empty(t, evaluation.Metrics.ProcessSlopes)
	require.Empty(t, evaluation.Metrics.GCPerHour)
	require.Empty(t, evaluation.Metrics.WorkingSetSlopes)
	require.False(t, gate(evaluation, "working-set/participant-1/execution/headroom").Insufficient, "headroom is a peak, not a trend")

	rendered := RenderSummary(evaluation)
	require.Contains(t, rendered, "| process/participant-1/execution/gc-pause | n/a |")
	require.Contains(t, rendered, "| working-set/participant-1/execution | n/a |")
}

func TestEvaluatePeersSingleParticipant(t *testing.T) {
	thresholds := DefaultThresholds()
	thresholds.Memory.MinSamples = 99
	start := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	samples := []Sample{
		steady(start, 100, 100, 0, 1<<30),
		steady(start.Add(time.Minute), 112, 112, 0, 1<<30),
	}
	for i := range samples {
		samples[i].Participants = samples[i].Participants[:1]
	}

	evaluation := Evaluate(samples, thresholds, Options{Participants: 1, SlotsPerEpoch: 8, Enforce: true})
	require.True(t, evaluation.Passed, gatesDetail(evaluation))
	peers := gate(evaluation, "peers/participant-1")
	require.True(t, peers.Passed)
	require.True(t, peers.Insufficient)
	require.Equal(t, "single participant; nothing to peer with", peers.Observed)
	require.Equal(t, "n/a", peers.Threshold)
	require.Contains(t, RenderSummary(evaluation), "| peers/participant-1 | n/a |")
	require.NotContains(t, names(evaluation), "peers/participant-2")
}

func TestEvaluatePeersZeroMinimumsAreNotJudged(t *testing.T) {
	thresholds := DefaultThresholds()
	thresholds.Memory.MinSamples = 99
	thresholds.Peers.MinExecutionPeers = 0
	thresholds.Peers.MinConsensusPeers = 0
	start := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	samples := []Sample{
		steady(start, 100, 100, 0, 1<<30),
		steady(start.Add(time.Minute), 112, 112, 0, 1<<30),
	}

	evaluation := Evaluate(samples, thresholds, Options{Participants: 2, SlotsPerEpoch: 8, Enforce: true})
	peers := gate(evaluation, "peers/participant-2")
	require.True(t, peers.Insufficient)
	require.Equal(t, "minimum peers resolve to 0; nothing to judge", peers.Observed)

	thresholds.Peers.MinExecutionPeers = -1
	thresholds.Peers.MinConsensusPeers = -1
	judged := Evaluate(samples, thresholds, Options{Participants: 2, SlotsPerEpoch: 8, Enforce: true})
	require.False(t, gate(judged, "peers/participant-2").Insufficient)
	require.False(t, gate(judged, "peers/participant-2").Passed, "two participants with zero peers are under-peered")
}

func TestMinPeers(t *testing.T) {
	require.Equal(t, 3, DefaultThresholds().MinPeers(4, -1))
	require.Equal(t, 2, DefaultThresholds().MinPeers(4, 2))
}

func TestParseQuantity(t *testing.T) {
	value, err := ParseQuantity("1536Mi")
	require.NoError(t, err)
	require.InDelta(t, 1536<<20, value, 1)

	value, err = ParseQuantity("250m")
	require.NoError(t, err)
	require.InDelta(t, 0.25, value, 1e-9)

	_, err = ParseQuantity("")
	require.Error(t, err)
}

func TestSlopePerHour(t *testing.T) {
	origin := time.Unix(0, 0).UTC()
	slope, ok := slopePerHour([]point{
		{origin, 100},
		{origin.Add(time.Hour), 200},
		{origin.Add(2 * time.Hour), 300},
	})
	require.True(t, ok)
	require.InDelta(t, 100, slope, 1e-9)

	_, ok = slopePerHour([]point{{origin, 1}})
	require.False(t, ok)
}

func TestPercentile(t *testing.T) {
	require.Equal(t, time.Duration(0), percentile(nil, 95))
	require.Equal(t, 30*time.Millisecond, percentile([]time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond}, 95))
}

func TestReadSamples(t *testing.T) {
	samples, err := ReadSamples(strings.NewReader(`{"phase":"warmup","at":"2026-09-03T12:00:00Z"}
{"phase":"steady","at":"2026-09-03T12:01:00Z"}
`))
	require.NoError(t, err)
	require.Equal(t, []Phase{PhaseWarmup, PhaseSteady}, []Phase{samples[0].Phase, samples[1].Phase})
}

func steady(at time.Time, head, slot uint64, peers int, rss float64) Sample {
	participant := func(index int) ParticipantSample {
		return ParticipantSample{
			Index: index, Head: head, HeadSlot: slot, FinalizedEpoch: slot / 8,
			ExecutionPeers: peers, ConsensusPeers: peers, Calls: 4,
			ReferenceHash: "0xaaa", ReferenceState: "0xbbb",
			Clients: map[Client]ClientMetrics{
				ClientExecution: {RSSBytes: rss, HeapBytes: rss / 2, Goroutines: 100, Scraped: true},
				ClientConsensus: {RSSBytes: rss / 2, HeapBytes: rss / 4, Goroutines: 80, Scraped: true},
			},
		}
	}
	return Sample{
		At: at, Phase: PhaseSteady, Reference: head - 2,
		Participants: []ParticipantSample{participant(1), participant(2)},
	}
}

// processSamples is two steady samples span apart with file descriptors,
// GC pause, GC count and container working set on participant 1 growing
// by 120 FDs, 18 ms, 500 collections and 10 MB.
func processSamples(start time.Time, span time.Duration) []Sample {
	first := steady(start, 100, 100, 10, 1<<30)
	second := steady(start.Add(span), 100+uint64(span.Minutes()*12), 100+uint64(span.Minutes()*12), 10, 1<<30)
	for _, sample := range []*Sample{&first, &second} {
		for i := range sample.Participants {
			stats := sample.Participants[i].Clients[ClientExecution]
			stats.OpenFDs = 100
			stats.GCPauseSec = 0.001
			stats.GCCount = 1000
			sample.Participants[i].Clients[ClientExecution] = stats
		}
		sample.Containers = []ContainerSample{{
			Participant: 1, Pod: "el-1", Container: "execution",
			WorkingSetBytes: 1 << 30, LimitBytes: 4 << 30,
		}}
	}
	second.Participants[0].Clients[ClientExecution] = ClientMetrics{
		RSSBytes: 1 << 30, HeapBytes: 1 << 29, Goroutines: 100, Scraped: true,
		OpenFDs: 220, GCPauseSec: 0.019, GCCount: 1500,
	}
	second.Containers[0].WorkingSetBytes = 1<<30 + 10<<20

	return []Sample{first, second}
}

func names(evaluation Evaluation) []string {
	result := make([]string, len(evaluation.Gates))
	for i, gate := range evaluation.Gates {
		result[i] = gate.Name
	}
	return result
}

func gate(evaluation Evaluation, name string) Gate {
	for _, candidate := range evaluation.Gates {
		if candidate.Name == name {
			return candidate
		}
	}
	return Gate{Name: name}
}

func gatesDetail(evaluation Evaluation) string {
	detail := ""
	for _, gate := range evaluation.Gates {
		if !gate.Passed {
			detail += gate.Name + ": " + gate.Observed + " " + gate.Detail + "\n"
		}
	}
	if detail == "" {
		return "all gates passed"
	}
	return detail
}
