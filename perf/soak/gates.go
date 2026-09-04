package soak

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Gate is one pass/fail rule with the evidence behind its verdict.
type Gate struct {
	Name      string `json:"name"`
	Passed    bool   `json:"passed"`
	Observed  string `json:"observed"`
	Threshold string `json:"threshold"`
	// Insufficient marks a gate that could not be judged (too few samples,
	// feature not enabled); it passes but the summary says why.
	Insufficient bool `json:"insufficient,omitempty"`
	// FirstBreach is when the gate first went out of bounds, for aligning
	// with logs and Grafana.
	FirstBreach *time.Time `json:"first_breach,omitempty"`
	Detail      string     `json:"detail,omitempty"`
}

// Evaluation is the verdict over the steady-state window.
type Evaluation struct {
	Passed        bool          `json:"passed"`
	Enforced      bool          `json:"enforced"`
	Samples       int           `json:"samples"`
	SteadySamples int           `json:"steady_samples"`
	SteadyWindow  time.Duration `json:"steady_window"`
	WarmupWindow  time.Duration `json:"warmup_window"`
	Gates         []Gate        `json:"gates"`
	// FirstBreach is the earliest breach across all gates.
	FirstBreach *time.Time `json:"first_breach,omitempty"`
	// ThresholdsDigest is the sha256 of the thresholds file this verdict
	// used. Week-over-week comparison refuses to run when it changes.
	ThresholdsDigest string `json:"thresholds_digest,omitempty"`
	// Provenance for apples-to-apples comparison. The Job image has no
	// .git; GITHUB_SHA is the fallback for qrl-tests.
	QRLTests       string            `json:"qrl_tests,omitempty"`
	PackageLocator string            `json:"qrl_package,omitempty"`
	Images         map[string]string `json:"images,omitempty"`
	Metrics        Metrics           `json:"metrics"`
}

// Metrics are the headline numbers a reader wants without reading gates.
type Metrics struct {
	HeadBlocksPerMinute map[int]float64          `json:"head_blocks_per_minute"`
	MissedSlotRate      float64                  `json:"missed_slot_rate"`
	MaxFinalityLag      uint64                   `json:"max_finality_lag_epochs"`
	RPCErrorRate        float64                  `json:"rpc_error_rate"`
	SplitSamples        int                      `json:"consensus_split_samples"`
	CanaryP50           time.Duration            `json:"canary_p50,omitempty"`
	CanaryP95           time.Duration            `json:"canary_p95,omitempty"`
	CanaryFailureRate   float64                  `json:"canary_failure_rate"`
	CanarySent          int                      `json:"canary_sent"`
	MemorySlopes        map[string]MemoryTrend   `json:"memory_slopes"`
	ProcessSlopes       map[string]MemoryTrend   `json:"process_slopes,omitempty"`
	GCPerHour           map[string]float64       `json:"gc_per_hour,omitempty"`
	WorkingSetSlopes    map[string]MemoryTrend   `json:"working_set_slopes,omitempty"`
	PeakWorkingSetShare map[string]float64       `json:"peak_working_set_share,omitempty"`
	Placement           []Placement              `json:"placement,omitempty"`
	Participants        map[int]ParticipantStats `json:"participants"`
}

type MemoryTrend struct {
	SlopeMBPerHour float64 `json:"slope_mb_per_hour"`
	FirstMB        float64 `json:"first_mb"`
	LastMB         float64 `json:"last_mb"`
	Samples        int     `json:"samples"`
}

type ParticipantStats struct {
	FirstHead      uint64 `json:"first_head"`
	LastHead       uint64 `json:"last_head"`
	FinalizedEpoch uint64 `json:"finalized_epoch"`
	MinExecPeers   int    `json:"min_execution_peers"`
	MinConsPeers   int    `json:"min_consensus_peers"`
}

// Options shape the evaluation for the network that was run.
type Options struct {
	Participants  int
	SlotsPerEpoch uint64
	// Enforce false turns a failing verdict into a passing one with the
	// breaches recorded, for shaking down thresholds on a new cluster.
	Enforce bool
	// LoadPercent zero means the canary and spammer-dependent gates are not
	// meaningful; canary gates still run if a canary was sent.
	LoadPercent int
	Placement   []Placement
}

// Evaluate judges the steady-state samples against the thresholds.
func Evaluate(samples []Sample, thresholds Thresholds, options Options) Evaluation {
	evaluation := Evaluation{
		Enforced:         options.Enforce,
		Samples:          len(samples),
		ThresholdsDigest: thresholds.Digest,
	}
	evaluation.Metrics.Placement = options.Placement

	steady := make([]Sample, 0, len(samples))
	for _, sample := range samples {
		if sample.Phase == PhaseSteady {
			steady = append(steady, sample)
		}
	}
	evaluation.SteadySamples = len(steady)
	if len(samples) > 0 && len(steady) > 0 {
		evaluation.WarmupWindow = steady[0].At.Sub(samples[0].At)
		evaluation.SteadyWindow = steady[len(steady)-1].At.Sub(steady[0].At)
	}

	if len(steady) < 2 {
		evaluation.Gates = []Gate{{
			Name: "steady-state", Passed: false,
			Observed:  fmt.Sprintf("%d steady samples", len(steady)),
			Threshold: "at least 2",
			Detail:    "the network never reached steady state, or the run ended during warm-up",
		}}
		evaluation.Passed = !options.Enforce
		return evaluation
	}

	evaluator := evaluator{steady: steady, thresholds: thresholds, options: options}
	evaluation.Metrics.Participants = evaluator.participantStats()
	evaluation.Gates = append(evaluation.Gates, evaluator.chainProgress(&evaluation.Metrics)...)
	evaluation.Gates = append(evaluation.Gates, evaluator.finality(&evaluation.Metrics))
	evaluation.Gates = append(evaluation.Gates, evaluator.peers()...)
	evaluation.Gates = append(evaluation.Gates, evaluator.rpc(&evaluation.Metrics))
	evaluation.Gates = append(evaluation.Gates, evaluator.consensusSplit(&evaluation.Metrics))
	evaluation.Gates = append(evaluation.Gates, evaluator.canary(&evaluation.Metrics)...)
	evaluation.Gates = append(evaluation.Gates, evaluator.memory(&evaluation.Metrics)...)
	evaluation.Gates = append(evaluation.Gates, evaluator.process(&evaluation.Metrics)...)
	evaluation.Gates = append(evaluation.Gates, evaluator.workingSet(&evaluation.Metrics)...)
	if options.Placement != nil {
		evaluation.Gates = append(evaluation.Gates, evaluator.placement())
	}

	evaluation.Passed = true
	for _, gate := range evaluation.Gates {
		if !gate.Passed {
			evaluation.Passed = false
			if gate.FirstBreach != nil && (evaluation.FirstBreach == nil || gate.FirstBreach.Before(*evaluation.FirstBreach)) {
				breach := *gate.FirstBreach
				evaluation.FirstBreach = &breach
			}
		}
	}
	if !options.Enforce {
		evaluation.Passed = true
	}
	return evaluation
}

type evaluator struct {
	steady     []Sample
	thresholds Thresholds
	options    Options
}

func (e evaluator) first() Sample { return e.steady[0] }
func (e evaluator) last() Sample  { return e.steady[len(e.steady)-1] }

func (e evaluator) participantStats() map[int]ParticipantStats {
	stats := make(map[int]ParticipantStats)
	for _, sample := range e.steady {
		for _, participant := range sample.Participants {
			current, seen := stats[participant.Index]
			if !seen {
				current = ParticipantStats{FirstHead: participant.Head, MinExecPeers: participant.ExecutionPeers, MinConsPeers: participant.ConsensusPeers}
			}
			if participant.Errors == 0 {
				current.LastHead = participant.Head
				current.FinalizedEpoch = participant.FinalizedEpoch
				current.MinExecPeers = min(current.MinExecPeers, participant.ExecutionPeers)
				current.MinConsPeers = min(current.MinConsPeers, participant.ConsensusPeers)
			}
			stats[participant.Index] = current
		}
	}
	return stats
}

func (e evaluator) chainProgress(metrics *Metrics) []Gate {
	metrics.HeadBlocksPerMinute = make(map[int]float64)
	minutes := e.last().At.Sub(e.first().At).Minutes()
	var gates []Gate
	for _, index := range e.participantIndexes() {
		first, last := e.participantIn(e.first(), index), e.participantIn(e.last(), index)
		rate := 0.0
		if minutes > 0 && last.Head >= first.Head {
			rate = float64(last.Head-first.Head) / minutes
		}
		metrics.HeadBlocksPerMinute[index] = rate

		gate := Gate{
			Name:      fmt.Sprintf("chain-progress/participant-%d", index),
			Passed:    rate >= e.thresholds.Chain.MinHeadBlocksPerMinute,
			Observed:  fmt.Sprintf("%.2f blocks/min (%d → %d)", rate, first.Head, last.Head),
			Threshold: fmt.Sprintf("≥ %.2f blocks/min", e.thresholds.Chain.MinHeadBlocksPerMinute),
		}
		// Stall detection: consecutive samples without head movement.
		stalled, previous := 0, uint64(0)
		for sampleIndex, sample := range e.steady {
			participant := e.participantIn(sample, index)
			if participant.Errors > 0 {
				continue
			}
			if sampleIndex > 0 && participant.Head <= previous {
				stalled++
				if stalled >= e.thresholds.Chain.MaxStalledSamples && gate.FirstBreach == nil {
					at := sample.At
					gate.FirstBreach = &at
					gate.Passed = false
					gate.Detail = fmt.Sprintf("head stalled at %d for %d consecutive samples", participant.Head, stalled)
				}
			} else {
				stalled = 0
			}
			previous = participant.Head
		}
		if !gate.Passed && gate.FirstBreach == nil {
			at := e.last().At
			gate.FirstBreach = &at
		}
		gates = append(gates, gate)
	}

	// Missed slots: the network-wide gap between slots elapsed and blocks
	// produced, measured on the participant with the lowest head.
	slots := e.minAcross(func(p ParticipantSample) uint64 { return p.HeadSlot })
	blocks := e.minAcross(func(p ParticipantSample) uint64 { return p.Head })
	firstSlots, lastSlots := slots[0], slots[1]
	firstBlocks, lastBlocks := blocks[0], blocks[1]
	missed := 0.0
	if lastSlots > firstSlots {
		elapsed := float64(lastSlots - firstSlots)
		produced := float64(0)
		if lastBlocks > firstBlocks {
			produced = float64(lastBlocks - firstBlocks)
		}
		missed = max(0, (elapsed-produced)/elapsed)
	}
	metrics.MissedSlotRate = missed
	gate := Gate{
		Name:      "chain-progress/missed-slots",
		Passed:    missed <= e.thresholds.Chain.MaxMissedSlotRate,
		Observed:  fmt.Sprintf("%.2f%% of %d slots", missed*100, lastSlots-firstSlots),
		Threshold: fmt.Sprintf("≤ %.2f%%", e.thresholds.Chain.MaxMissedSlotRate*100),
	}
	if lastSlots <= firstSlots {
		gate.Insufficient = true
		gate.Passed = true
		gate.Detail = "no consensus slot data"
	}
	return append(gates, gate)
}

func (e evaluator) finality(metrics *Metrics) Gate {
	gate := Gate{Name: "finality/lag", Passed: true, Threshold: fmt.Sprintf("≤ %d epochs", e.thresholds.Chain.MaxFinalityLagEpochs)}
	if e.options.SlotsPerEpoch == 0 {
		gate.Insufficient, gate.Detail = true, "slots per epoch unknown"
		return gate
	}
	for _, sample := range e.steady {
		for _, participant := range sample.Participants {
			if participant.Errors > 0 || participant.HeadSlot == 0 {
				continue
			}
			headEpoch := participant.HeadSlot / e.options.SlotsPerEpoch
			if headEpoch < participant.FinalizedEpoch {
				continue
			}
			lag := headEpoch - participant.FinalizedEpoch
			metrics.MaxFinalityLag = max(metrics.MaxFinalityLag, lag)
			if lag > e.thresholds.Chain.MaxFinalityLagEpochs && gate.FirstBreach == nil {
				at := sample.At
				gate.FirstBreach = &at
				gate.Passed = false
				gate.Detail = fmt.Sprintf("participant %d at head epoch %d with finalized epoch %d", participant.Index, headEpoch, participant.FinalizedEpoch)
			}
		}
	}
	gate.Observed = fmt.Sprintf("max lag %d epochs", metrics.MaxFinalityLag)
	return gate
}

func (e evaluator) peers() []Gate {
	minExecution := e.thresholds.MinPeers(e.options.Participants, e.thresholds.Peers.MinExecutionPeers)
	minConsensus := e.thresholds.MinPeers(e.options.Participants, e.thresholds.Peers.MinConsensusPeers)
	var gates []Gate
	for _, index := range e.participantIndexes() {
		breaches, judged := 0, 0
		var first *time.Time
		lowExec, lowCons := -1, -1
		for _, sample := range e.steady {
			participant := e.participantIn(sample, index)
			if participant.Errors > 0 {
				continue
			}
			judged++
			if lowExec < 0 || participant.ExecutionPeers < lowExec {
				lowExec = participant.ExecutionPeers
			}
			if lowCons < 0 || participant.ConsensusPeers < lowCons {
				lowCons = participant.ConsensusPeers
			}
			if participant.ExecutionPeers < minExecution || participant.ConsensusPeers < minConsensus {
				breaches++
				if first == nil {
					at := sample.At
					first = &at
				}
			}
		}
		rate := ratio(breaches, judged)
		gate := Gate{
			Name:      fmt.Sprintf("peers/participant-%d", index),
			Passed:    rate <= e.thresholds.Peers.MaxBreachRate,
			Observed:  fmt.Sprintf("under-peered in %d/%d samples (min EL %d, min CL %d)", breaches, judged, lowExec, lowCons),
			Threshold: fmt.Sprintf("EL ≥ %d, CL ≥ %d in ≥ %.0f%% of samples", minExecution, minConsensus, (1-e.thresholds.Peers.MaxBreachRate)*100),
		}
		if !gate.Passed {
			gate.FirstBreach = first
		}
		gates = append(gates, gate)
	}
	return gates
}

func (e evaluator) rpc(metrics *Metrics) Gate {
	calls, errs := 0, 0
	var first *time.Time
	for _, sample := range e.steady {
		for _, participant := range sample.Participants {
			calls += participant.Calls
			errs += participant.Errors
			if participant.Errors > 0 && first == nil {
				at := sample.At
				first = &at
			}
		}
	}
	rate := ratio(errs, calls)
	metrics.RPCErrorRate = rate
	gate := Gate{
		Name:      "rpc/error-rate",
		Passed:    rate <= e.thresholds.RPC.MaxErrorRate,
		Observed:  fmt.Sprintf("%d/%d calls failed (%.3f%%)", errs, calls, rate*100),
		Threshold: fmt.Sprintf("≤ %.2f%%", e.thresholds.RPC.MaxErrorRate*100),
	}
	if !gate.Passed {
		gate.FirstBreach = first
		gate.Detail = e.firstFaults(3)
	}
	return gate
}

func (e evaluator) consensusSplit(metrics *Metrics) Gate {
	splits := 0
	var first *time.Time
	var detail string
	for _, sample := range e.steady {
		if sample.Reference == 0 {
			continue
		}
		hashes := make(map[string][]int)
		for _, participant := range sample.Participants {
			if participant.ReferenceHash == "" {
				continue
			}
			key := participant.ReferenceHash + "/" + participant.ReferenceState
			hashes[key] = append(hashes[key], participant.Index)
		}
		if len(hashes) > 1 {
			splits++
			if first == nil {
				at := sample.At
				first = &at
				var views []string
				for key, members := range hashes {
					views = append(views, fmt.Sprintf("%v→%s", members, shorten(key)))
				}
				sort.Strings(views)
				detail = fmt.Sprintf("block %d: %s", sample.Reference, strings.Join(views, ", "))
			}
		}
	}
	metrics.SplitSamples = splits
	gate := Gate{
		Name:      "consensus/split",
		Passed:    splits <= e.thresholds.Consensus.MaxSplitSamples,
		Observed:  fmt.Sprintf("%d samples with divergent reference blocks", splits),
		Threshold: fmt.Sprintf("≤ %d", e.thresholds.Consensus.MaxSplitSamples),
		Detail:    detail,
	}
	if !gate.Passed {
		gate.FirstBreach = first
	}
	return gate
}

func (e evaluator) canary(metrics *Metrics) []Gate {
	var latencies []time.Duration
	sent, failed := 0, 0
	var first *time.Time
	for _, sample := range e.steady {
		if sample.Canary == nil {
			continue
		}
		sent++
		if !sample.Canary.Included {
			failed++
			if first == nil {
				at := sample.At
				first = &at
			}
			continue
		}
		latencies = append(latencies, sample.Canary.Latency)
	}
	metrics.CanarySent = sent
	if sent == 0 {
		return []Gate{{Name: "canary/latency", Passed: true, Insufficient: true, Observed: "no canaries sent", Threshold: "n/a"}}
	}
	metrics.CanaryP50 = percentile(latencies, 50)
	metrics.CanaryP95 = percentile(latencies, 95)
	metrics.CanaryFailureRate = ratio(failed, sent)

	latency := Gate{
		Name:      "canary/latency",
		Passed:    len(latencies) > 0 && metrics.CanaryP50 <= e.thresholds.Canary.P50Max && metrics.CanaryP95 <= e.thresholds.Canary.P95Max,
		Observed:  fmt.Sprintf("p50 %s, p95 %s over %d included", metrics.CanaryP50.Round(time.Millisecond), metrics.CanaryP95.Round(time.Millisecond), len(latencies)),
		Threshold: fmt.Sprintf("p50 ≤ %s, p95 ≤ %s", e.thresholds.Canary.P50Max, e.thresholds.Canary.P95Max),
	}
	if !latency.Passed {
		at := e.last().At
		latency.FirstBreach = &at
	}
	failures := Gate{
		Name:      "canary/failures",
		Passed:    metrics.CanaryFailureRate <= e.thresholds.Canary.MaxFailureRate,
		Observed:  fmt.Sprintf("%d/%d not included within %s", failed, sent, e.thresholds.Canary.Timeout),
		Threshold: fmt.Sprintf("≤ %.1f%%", e.thresholds.Canary.MaxFailureRate*100),
	}
	if !failures.Passed {
		failures.FirstBreach = first
	}
	return []Gate{latency, failures}
}

func (e evaluator) memory(metrics *Metrics) []Gate {
	metrics.MemorySlopes = make(map[string]MemoryTrend)
	var gates []Gate
	for _, index := range e.participantIndexes() {
		for _, client := range []Client{ClientExecution, ClientConsensus, ClientValidator} {
			var rss, heap, goroutines []point
			for _, sample := range e.steady {
				participant := e.participantIn(sample, index)
				stats, scraped := participant.Clients[client]
				if !scraped || !stats.Scraped {
					continue
				}
				if stats.RSSBytes > 0 {
					rss = append(rss, point{sample.At, stats.RSSBytes})
				}
				if stats.HeapBytes > 0 {
					heap = append(heap, point{sample.At, stats.HeapBytes})
				}
				if stats.Goroutines > 0 {
					goroutines = append(goroutines, point{sample.At, stats.Goroutines})
				}
			}
			if len(rss) == 0 && len(heap) == 0 && len(goroutines) == 0 {
				continue
			}
			prefix := fmt.Sprintf("memory/participant-%d/%s", index, client)
			rssLimit := map[Client]float64{
				ClientExecution: e.thresholds.Memory.ExecutionRSSSlopeMaxMBPerHour,
				ClientConsensus: e.thresholds.Memory.ConsensusRSSSlopeMaxMBPerHour,
				ClientValidator: e.thresholds.Memory.ValidatorRSSSlopeMaxMBPerHour,
			}[client]
			gates = append(gates, e.trendGate(prefix+"/rss", rss, rssLimit, bytesToMB, "MB/h", metrics.MemorySlopes))
			gates = append(gates, e.trendGate(prefix+"/heap", heap, e.thresholds.Memory.HeapSlopeMaxMBPerHour, bytesToMB, "MB/h", metrics.MemorySlopes))
			gates = append(gates, e.trendGate(prefix+"/goroutines", goroutines, e.thresholds.Memory.GoroutineSlopeMaxPerHour, func(v float64) float64 { return v }, "/h", nil))
		}
	}
	if len(gates) == 0 {
		gates = append(gates, Gate{Name: "memory/trend", Passed: true, Insufficient: true, Observed: "no client metrics scraped", Threshold: "n/a"})
	}
	return gates
}

func (e evaluator) process(metrics *Metrics) []Gate {
	metrics.ProcessSlopes = make(map[string]MemoryTrend)
	metrics.GCPerHour = make(map[string]float64)
	var gates []Gate
	for _, index := range e.participantIndexes() {
		for _, client := range []Client{ClientExecution, ClientConsensus, ClientValidator} {
			var fds, pauses, counts []point
			for _, sample := range e.steady {
				participant := e.participantIn(sample, index)
				stats, scraped := participant.Clients[client]
				if !scraped || !stats.Scraped {
					continue
				}
				if stats.OpenFDs > 0 {
					fds = append(fds, point{sample.At, stats.OpenFDs})
				}
				if stats.GCPauseSec > 0 {
					pauses = append(pauses, point{sample.At, stats.GCPauseSec})
				}
				if stats.GCCount > 0 {
					counts = append(counts, point{sample.At, stats.GCCount})
				}
			}
			prefix := fmt.Sprintf("process/participant-%d/%s", index, client)
			if len(fds) > 0 {
				gates = append(gates, e.trendGate(prefix+"/fds", fds, e.thresholds.Memory.OpenFDSlopeMaxPerHour, func(v float64) float64 { return v }, "/h", metrics.ProcessSlopes))
			}
			if len(pauses) > 0 {
				gates = append(gates, e.trendGate(prefix+"/gc-pause", pauses, e.thresholds.Memory.GCPauseSlopeMaxMSPerHour, secondsToMS, "ms/h", metrics.ProcessSlopes))
			}
			if gate, key, rate, ok := e.gcRateGate(prefix+"/gc-rate", counts); ok {
				metrics.GCPerHour[key] = rate
				gates = append(gates, gate)
			}
		}
	}
	return gates
}

func (e evaluator) gcRateGate(name string, points []point) (Gate, string, float64, bool) {
	gate := Gate{Name: name, Threshold: fmt.Sprintf("≤ %.0f GC/h", e.thresholds.Memory.MaxGCPerHour)}
	if len(points) < e.thresholds.Memory.MinSamples {
		return Gate{}, "", 0, false
	}
	hours := points[len(points)-1].at.Sub(points[0].at).Hours()
	if hours <= 0 {
		return Gate{}, "", 0, false
	}
	rate := (points[len(points)-1].value - points[0].value) / hours
	if rate < 0 {
		rate = 0
	}
	gate.Passed = rate <= e.thresholds.Memory.MaxGCPerHour
	gate.Observed = fmt.Sprintf("%.0f GC/h over %d samples", rate, len(points))
	if !gate.Passed {
		at := points[len(points)-1].at
		gate.FirstBreach = &at
	}
	key := strings.TrimPrefix(name, "process/")
	return gate, key, rate, true
}

func secondsToMS(seconds float64) float64 { return seconds * 1000 }

func (e evaluator) workingSet(metrics *Metrics) []Gate {
	series := make(map[string][]point)
	limits := make(map[string]float64)
	peaks := make(map[string]float64)
	for _, sample := range e.steady {
		for _, container := range sample.Containers {
			key := fmt.Sprintf("participant-%d/%s", container.Participant, container.Container)
			if container.Participant == 0 {
				key = container.Pod + "/" + container.Container
			}
			series[key] = append(series[key], point{sample.At, container.WorkingSetBytes})
			if container.LimitBytes > 0 {
				limits[key] = container.LimitBytes
				peaks[key] = max(peaks[key], container.WorkingSetBytes/container.LimitBytes)
			}
		}
	}
	if len(series) == 0 {
		return nil
	}
	metrics.WorkingSetSlopes = make(map[string]MemoryTrend)
	metrics.PeakWorkingSetShare = peaks
	keys := make([]string, 0, len(series))
	for key := range series {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var gates []Gate
	for _, key := range keys {
		gates = append(gates, e.trendGate("working-set/"+key, series[key], e.thresholds.Memory.WorkingSetSlopeMaxMBPerHour, bytesToMB, "MB/h", metrics.WorkingSetSlopes))
		if limit, bounded := limits[key]; bounded {
			gate := Gate{
				Name:      "working-set/" + key + "/headroom",
				Passed:    peaks[key] <= e.thresholds.Memory.MaxWorkingSetShareOfLimit,
				Observed:  fmt.Sprintf("peak %.1f%% of %.0f MB limit", peaks[key]*100, bytesToMB(limit)),
				Threshold: fmt.Sprintf("≤ %.0f%%", e.thresholds.Memory.MaxWorkingSetShareOfLimit*100),
			}
			if !gate.Passed {
				at := e.last().At
				gate.FirstBreach = &at
			}
			gates = append(gates, gate)
		}
	}
	return gates
}

func (e evaluator) memoryWindowTooShort(name string, points []point) (time.Duration, bool) {
	if e.thresholds.Memory.MinWindow <= 0 || !strings.HasPrefix(name, "memory/") || len(points) < 2 {
		return 0, false
	}
	window := points[len(points)-1].at.Sub(points[0].at)
	return window, window < e.thresholds.Memory.MinWindow
}

func (e evaluator) trendGate(name string, points []point, limit float64, convert func(float64) float64, unit string, into map[string]MemoryTrend) Gate {
	gate := Gate{Name: name, Threshold: fmt.Sprintf("slope ≤ %.0f %s", limit, unit)}
	if len(points) < e.thresholds.Memory.MinSamples {
		gate.Passed, gate.Insufficient = true, true
		gate.Observed = fmt.Sprintf("%d samples", len(points))
		gate.Detail = fmt.Sprintf("need %d samples for a trend", e.thresholds.Memory.MinSamples)
		return gate
	}
	if window, short := e.memoryWindowTooShort(name, points); short {
		gate.Passed, gate.Insufficient = true, true
		gate.Observed = fmt.Sprintf("window %s over %d samples", window.Round(time.Second), len(points))
		gate.Detail = fmt.Sprintf("need %s of steady samples before judging a memory slope", e.thresholds.Memory.MinWindow)
		return gate
	}
	slope, ok := slopePerHour(points)
	if !ok {
		gate.Passed, gate.Insufficient = true, true
		gate.Observed = "degenerate series"
		return gate
	}
	slope = convert(slope)
	trend := MemoryTrend{
		SlopeMBPerHour: slope,
		FirstMB:        convert(points[0].value),
		LastMB:         convert(points[len(points)-1].value),
		Samples:        len(points),
	}
	if into != nil {
		key := name
		for _, prefix := range []string{"memory/", "working-set/", "process/"} {
			key = strings.TrimPrefix(key, prefix)
		}
		into[key] = trend
	}
	gate.Passed = slope <= limit
	gate.Observed = fmt.Sprintf("slope %.1f %s (%.0f → %.0f over %d samples)", slope, unit, trend.FirstMB, trend.LastMB, len(points))
	if !gate.Passed {
		at := points[len(points)-1].at
		gate.FirstBreach = &at
	}
	return gate
}

func (e evaluator) placement() Gate {
	gate := Gate{Name: "placement/one-participant-per-node", Passed: true, Threshold: "every participant pinned to its own labelled node"}
	var pinned, unpinned []string
	for _, placement := range e.options.Placement {
		label := fmt.Sprintf("%d→%s", placement.Participant, placement.Node)
		if placement.Pinned {
			pinned = append(pinned, label)
		} else {
			unpinned = append(unpinned, label)
			gate.Passed = false
		}
	}
	gate.Observed = fmt.Sprintf("%d pinned, %d not", len(pinned), len(unpinned))
	if len(unpinned) > 0 {
		gate.Detail = "not pinned: " + strings.Join(unpinned, ", ")
		at := e.first().At
		gate.FirstBreach = &at
	}
	return gate
}

func (e evaluator) participantIndexes() []int {
	seen := make(map[int]bool)
	var indexes []int
	for _, sample := range e.steady {
		for _, participant := range sample.Participants {
			if !seen[participant.Index] {
				seen[participant.Index] = true
				indexes = append(indexes, participant.Index)
			}
		}
	}
	sort.Ints(indexes)
	return indexes
}

func (e evaluator) participantIn(sample Sample, index int) ParticipantSample {
	for _, participant := range sample.Participants {
		if participant.Index == index {
			return participant
		}
	}
	return ParticipantSample{Index: index, Errors: 1}
}

// minAcross returns [first, last] of the minimum over participants of the
// selected value, skipping errored participants.
func (e evaluator) minAcross(pick func(ParticipantSample) uint64) [2]uint64 {
	minimum := func(sample Sample) uint64 {
		result, found := uint64(0), false
		for _, participant := range sample.Participants {
			if participant.Errors > 0 {
				continue
			}
			if value := pick(participant); !found || value < result {
				result, found = value, true
			}
		}
		return result
	}
	return [2]uint64{minimum(e.first()), minimum(e.last())}
}

func (e evaluator) firstFaults(limit int) string {
	var faults []string
	for _, sample := range e.steady {
		for _, participant := range sample.Participants {
			for _, fault := range participant.Faults {
				faults = append(faults, fmt.Sprintf("participant %d: %s", participant.Index, fault))
				if len(faults) == limit {
					return strings.Join(faults, "; ")
				}
			}
		}
	}
	return strings.Join(faults, "; ")
}

func shorten(key string) string {
	if len(key) <= 20 {
		return key
	}
	return key[:10] + "…" + key[len(key)-8:]
}
