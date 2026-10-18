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
