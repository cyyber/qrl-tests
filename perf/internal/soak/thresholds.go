// Package soak samples a long-running network, evaluates the samples against
// versioned gates, and writes the verdict in the shape the reports contract
// expects. It knows nothing about the CLI; qrltest soak in perf/soak drives it.
package soak

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"go.yaml.in/yaml/v3"
)

//go:embed thresholds.yaml
var defaultThresholdsYAML []byte

type Thresholds struct {
	Version int `yaml:"version" json:"version"`
	// Digest identifies the exact thresholds file a verdict was judged with.
	Digest string `yaml:"-" json:"digest"`

	Warmup struct {
		FinalizedEpochs uint64        `yaml:"finalized_epochs" json:"finalized_epochs"`
		Timeout         time.Duration `yaml:"timeout" json:"timeout"`
	} `yaml:"warmup" json:"warmup"`

	Chain struct {
		MinHeadBlocksPerMinute float64 `yaml:"min_head_blocks_per_minute" json:"min_head_blocks_per_minute"`
		MaxMissedSlotRate      float64 `yaml:"max_missed_slot_rate" json:"max_missed_slot_rate"`
		MaxFinalityLagEpochs   uint64  `yaml:"max_finality_lag_epochs" json:"max_finality_lag_epochs"`
		MaxStalledSamples      int     `yaml:"max_stalled_samples" json:"max_stalled_samples"`
	} `yaml:"chain" json:"chain"`

	Peers struct {
		MinExecutionPeers int     `yaml:"min_execution_peers" json:"min_execution_peers"`
		MinConsensusPeers int     `yaml:"min_consensus_peers" json:"min_consensus_peers"`
		MaxBreachRate     float64 `yaml:"max_breach_rate" json:"max_breach_rate"`
	} `yaml:"peers" json:"peers"`

	RPC struct {
		MaxErrorRate float64 `yaml:"max_error_rate" json:"max_error_rate"`
	} `yaml:"rpc" json:"rpc"`

	Consensus struct {
		MaxSplitSamples int `yaml:"max_split_samples" json:"max_split_samples"`
	} `yaml:"consensus" json:"consensus"`

	Canary struct {
		P50Max         time.Duration `yaml:"p50_max" json:"p50_max"`
		P95Max         time.Duration `yaml:"p95_max" json:"p95_max"`
		Timeout        time.Duration `yaml:"timeout" json:"timeout"`
		MaxFailureRate float64       `yaml:"max_failure_rate" json:"max_failure_rate"`
	} `yaml:"canary" json:"canary"`

	Memory struct {
		ExecutionRSSSlopeMaxMBPerHour float64 `yaml:"execution_rss_slope_max_mb_per_hour" json:"execution_rss_slope_max_mb_per_hour"`
		ConsensusRSSSlopeMaxMBPerHour float64 `yaml:"consensus_rss_slope_max_mb_per_hour" json:"consensus_rss_slope_max_mb_per_hour"`
		ValidatorRSSSlopeMaxMBPerHour float64 `yaml:"validator_rss_slope_max_mb_per_hour" json:"validator_rss_slope_max_mb_per_hour"`
		HeapSlopeMaxMBPerHour         float64 `yaml:"heap_slope_max_mb_per_hour" json:"heap_slope_max_mb_per_hour"`
		GoroutineSlopeMaxPerHour      float64 `yaml:"goroutine_slope_max_per_hour" json:"goroutine_slope_max_per_hour"`
		WorkingSetSlopeMaxMBPerHour   float64 `yaml:"working_set_slope_max_mb_per_hour" json:"working_set_slope_max_mb_per_hour"`
		MaxWorkingSetShareOfLimit     float64 `yaml:"max_working_set_share_of_limit" json:"max_working_set_share_of_limit"`
		MinSamples                    int     `yaml:"min_samples" json:"min_samples"`
	} `yaml:"memory" json:"memory"`

	Metrics struct {
		RSS        []string `yaml:"rss" json:"rss"`
		Heap       []string `yaml:"heap" json:"heap"`
		Goroutines []string `yaml:"goroutines" json:"goroutines"`
	} `yaml:"metrics" json:"metrics"`
}

// DefaultThresholds returns the embedded thresholds file.
func DefaultThresholds() Thresholds {
	thresholds, err := ParseThresholds(defaultThresholdsYAML)
	if err != nil {
		panic(fmt.Sprintf("embedded soak thresholds are invalid: %v", err))
	}
	return thresholds
}

// LoadThresholds reads an override file, falling back to the embedded
// defaults when path is empty.
func LoadThresholds(path string) (Thresholds, error) {
	if path == "" {
		return DefaultThresholds(), nil
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return Thresholds{}, fmt.Errorf("read soak thresholds: %w", err)
	}
	thresholds, err := ParseThresholds(payload)
	if err != nil {
		return Thresholds{}, fmt.Errorf("parse soak thresholds %s: %w", path, err)
	}
	return thresholds, nil
}

func ParseThresholds(payload []byte) (Thresholds, error) {
	var thresholds Thresholds
	decoder := yaml.NewDecoder(bytesReader(payload))
	decoder.KnownFields(true)
	if err := decoder.Decode(&thresholds); err != nil {
		return Thresholds{}, err
	}
	if thresholds.Version < 1 {
		return Thresholds{}, fmt.Errorf("version must be at least 1, got %d", thresholds.Version)
	}
	if thresholds.Memory.MinSamples < 2 {
		return Thresholds{}, fmt.Errorf("memory.min_samples must be at least 2, got %d", thresholds.Memory.MinSamples)
	}
	if len(thresholds.Metrics.RSS) == 0 || len(thresholds.Metrics.Heap) == 0 || len(thresholds.Metrics.Goroutines) == 0 {
		return Thresholds{}, fmt.Errorf("metrics.rss, metrics.heap and metrics.goroutines must each name at least one metric")
	}
	sum := sha256.Sum256(payload)
	thresholds.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return thresholds, nil
}

// MinPeers resolves the -1 sentinel to "every other participant".
func (thresholds Thresholds) MinPeers(participants int, configured int) int {
	if configured >= 0 {
		return configured
	}
	return max(0, participants-1)
}
