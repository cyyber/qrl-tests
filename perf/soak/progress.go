package soak

import (
	"context"
	"fmt"
	"io"
	"time"
)

// ProgressReporter publishes live soak numbers while the window runs.
type ProgressReporter interface {
	Report(ctx context.Context, sample Sample, n int) error
}

// LogProgress writes one line per sample, so the Actions job log shows how
// the network is doing while the soak runs.
type LogProgress struct {
	Out io.Writer
}

func (progress LogProgress) Report(_ context.Context, sample Sample, n int) error {
	if progress.Out == nil {
		return nil
	}
	_, err := fmt.Fprintln(progress.Out, ProgressLine(sample, n))
	return err
}

// ProgressLine summarizes one sample: the lowest finalized epoch, the
// highest head slot, the mean transactions in each head, and the largest
// execution-client RSS.
func ProgressLine(sample Sample, n int) string {
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
	return fmt.Sprintf("%s phase=%s samples=%d finalized=%d head_slot=%d tx/slot=%d el_rss=%.0fMB",
		sample.At.UTC().Format(time.TimeOnly+"Z"), sample.Phase, n, finalized, headSlot, txPerSlot, elRSS/(1<<20))
}
