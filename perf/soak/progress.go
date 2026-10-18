package soak

import (
	"context"
	"fmt"
	"strconv"
)

// ProgressReporter publishes live soak numbers so the heartbeat workflow
// can refresh the check run without holding a runner.
type ProgressReporter interface {
	Report(ctx context.Context, sample Sample, n int) error
}

// JobProgress patches annotations on the in-cluster soak Job.
type JobProgress struct {
	Kube *Kube
	Name string
}

func (progress *JobProgress) Report(ctx context.Context, sample Sample, n int) error {
	if progress == nil || progress.Kube == nil || progress.Name == "" {
		return nil
	}
	return progress.Kube.PatchJobAnnotations(ctx, progress.Name, LiveAnnotations(sample, n))
}

func (progress *JobProgress) reportPhase(ctx context.Context, phase string) error {
	if progress == nil || progress.Kube == nil || progress.Name == "" {
		return nil
	}
	return progress.Kube.PatchJobAnnotations(ctx, progress.Name, map[string]string{
		"qrl.io/phase": phase,
	})
}

// LiveAnnotations is what soak-heartbeat reads off the Job.
func LiveAnnotations(sample Sample, n int) map[string]string {
	var finalized, headSlot, txSum, txN uint64
	var elRSS float64
	for i, participant := range sample.Participants {
		if i == 0 || participant.FinalizedEpoch < finalized {
			finalized = participant.FinalizedEpoch
		}
		if participant.HeadSlot > headSlot {
			headSlot = participant.HeadSlot
		}
		if participant.TxInHead > 0 {
			txSum += participant.TxInHead
			txN++
		}
		if stats, ok := participant.Clients[ClientExecution]; ok && stats.RSSBytes > elRSS {
			elRSS = stats.RSSBytes
		}
	}
	txPerSlot := uint64(0)
	if txN > 0 {
		txPerSlot = txSum / txN
	}
	return map[string]string{
		"qrl.io/phase":           string(sample.Phase),
		"qrl.io/samples":         strconv.Itoa(n),
		"qrl.io/finalized-epoch": strconv.FormatUint(finalized, 10),
		"qrl.io/head-slot":       strconv.FormatUint(headSlot, 10),
		"qrl.io/tx-per-slot":     strconv.FormatUint(txPerSlot, 10),
		"qrl.io/el-rss-mb":       fmt.Sprintf("%.0f", elRSS/(1<<20)),
	}
}
