package testsuite

import (
	"context"
	"time"
)

// SleepUntil waits until deadline, or until ctx ends and returns its error.
func SleepUntil(ctx context.Context, deadline time.Time) error {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
