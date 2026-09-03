package soak

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// CanarySender submits one deterministic transfer and waits for its receipt.
// The soak runner implements it with the dev wallet; the sampler only records
// the outcome.
type CanarySender interface {
	Send(ctx context.Context, timeout time.Duration) Canary
}

// Sampler takes periodic samples of the network and streams them to a sink.
type Sampler struct {
	Participants []Endpoints
	Thresholds   Thresholds
	Interval     time.Duration
	// Kube enables container usage sampling; nil on Docker.
	Kube *Kube
	// Canary enables the inclusion-latency probe; nil disables it.
	Canary CanarySender
	// SlotsPerEpoch is needed to reason about finality lag during warm-up.
	SlotsPerEpoch uint64
	// Out receives every sample as one JSON line.
	Out io.Writer
	// Log receives progress; nil discards.
	Log *log.Logger
	// HTTP overrides the probe client; nil uses a 15 s-timeout default.
	HTTP *http.Client

	probe probe
	pods  []Pod
}

// Run samples until duration elapses or ctx ends, returning every sample
// taken. A cancelled context is not an error: the samples collected so far
// are still evaluated so an aborted run reports what it saw.
func (sampler *Sampler) Run(ctx context.Context, duration time.Duration) ([]Sample, error) {
	if sampler.Interval <= 0 {
		return nil, errors.New("sampler interval must be positive")
	}
	if len(sampler.Participants) == 0 {
		return nil, errors.New("sampler has no participants")
	}
	sampler.probe = newProbe(sampler.HTTP)

	deadline := time.Now().Add(duration)
	warm := false
	var samples []Sample
	ticker := time.NewTicker(sampler.Interval)
	defer ticker.Stop()
	for {
		sample := sampler.Sample(ctx)
		if !warm && sampler.isWarm(sample) {
			warm = true
			sampler.logf("warm-up complete after %d samples", len(samples)+1)
		}
		sample.Phase = PhaseWarmup
		if warm {
			sample.Phase = PhaseSteady
		}
		samples = append(samples, sample)
		if err := sampler.emit(sample); err != nil {
			return samples, err
		}
		if !warm && time.Since(samples[0].At) > sampler.Thresholds.Warmup.Timeout {
			return samples, fmt.Errorf("network did not warm up within %s", sampler.Thresholds.Warmup.Timeout)
		}
		if time.Now().After(deadline) {
			return samples, nil
		}
		select {
		case <-ctx.Done():
			return samples, nil
		case <-ticker.C:
		}
	}
}

// Sample takes one round: heads first, then the shared reference block,
// metrics, container usage, and the canary.
func (sampler *Sampler) Sample(ctx context.Context) Sample {
	if sampler.probe.http == nil {
		sampler.probe = newProbe(sampler.HTTP)
	}
	sample := Sample{At: time.Now().UTC(), Participants: make([]ParticipantSample, len(sampler.Participants))}

	var group sync.WaitGroup
	for index, endpoints := range sampler.Participants {
		group.Add(1)
		go func() {
			defer group.Done()
			sample.Participants[index] = sampler.probe.head(ctx, endpoints)
		}()
	}
	group.Wait()

	// The reference block trails the lowest head by two so every participant
	// has it and it is not subject to a same-slot reorg.
	lowest := uint64(0)
	for _, participant := range sample.Participants {
		if participant.Head == 0 {
			continue
		}
		if lowest == 0 || participant.Head < lowest {
			lowest = participant.Head
		}
	}
	if lowest > 2 {
		sample.Reference = lowest - 2
	}
	for index, endpoints := range sampler.Participants {
		group.Add(1)
		go func() {
			defer group.Done()
			participant := &sample.Participants[index]
			if sample.Reference > 0 && participant.Head > 0 {
				sampler.probe.reference(ctx, endpoints, participant, sample.Reference)
			}
			sampler.probe.metrics(ctx, endpoints, participant, sampler.Thresholds)
		}()
	}
	group.Wait()

	if sampler.Kube != nil {
		if pods, err := sampler.Kube.Pods(ctx); err == nil {
			sampler.pods = pods
		} else {
			sampler.logf("list pods: %v", err)
		}
		if usage, err := sampler.Kube.ContainerUsage(ctx, sampler.pods); err == nil {
			sample.Containers = usage
		} else {
			sampler.logf("container usage: %v", err)
		}
	}

	if sampler.Canary != nil {
		canary := sampler.Canary.Send(ctx, sampler.Thresholds.Canary.Timeout)
		sample.Canary = &canary
	}
	return sample
}

// isWarm holds once every participant has finalized the configured number
// of epochs and is not syncing.
func (sampler *Sampler) isWarm(sample Sample) bool {
	for _, participant := range sample.Participants {
		if participant.Errors > 0 || participant.Syncing {
			return false
		}
		if participant.FinalizedEpoch < sampler.Thresholds.Warmup.FinalizedEpochs {
			return false
		}
	}
	return true
}

func (sampler *Sampler) emit(sample Sample) error {
	if sampler.Out == nil {
		return nil
	}
	line, err := json.Marshal(sample)
	if err != nil {
		return err
	}
	_, err = sampler.Out.Write(append(line, '\n'))
	return err
}

func (sampler *Sampler) logf(format string, args ...any) {
	if sampler.Log != nil {
		sampler.Log.Printf(format, args...)
	}
}
