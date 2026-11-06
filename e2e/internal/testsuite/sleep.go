package testsuite

import (
	"context"
	"time"
)

// SleepUntil waits until deadline, or until ctx ends and returns its error. A
// ctx that has already ended wins over a deadline that has already passed.
func SleepUntil(ctx context.Context, deadline time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
