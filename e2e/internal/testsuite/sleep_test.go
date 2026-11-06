package testsuite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSleepUntil(t *testing.T) {
	start := time.Now()
	require.NoError(t, SleepUntil(t.Context(), start.Add(100*time.Millisecond)))
	require.GreaterOrEqual(t, time.Since(start), 100*time.Millisecond)

	// A deadline already past returns nil.
	require.NoError(t, SleepUntil(t.Context(), start.Add(-time.Hour)))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, SleepUntil(ctx, time.Now().Add(time.Hour)), context.Canceled)
	// An ended ctx wins even when the deadline has passed too.
	require.ErrorIs(t, SleepUntil(ctx, start.Add(-time.Hour)), context.Canceled)
}
