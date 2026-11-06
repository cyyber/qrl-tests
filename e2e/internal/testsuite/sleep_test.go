package testsuite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSleepUntil(t *testing.T) {
	start := time.Now()
	require.NoError(t, SleepUntil(t.Context(), start.Add(20*time.Millisecond)))
	require.GreaterOrEqual(t, time.Since(start), 20*time.Millisecond)

	// A deadline already past returns at once.
	require.NoError(t, SleepUntil(t.Context(), start.Add(-time.Hour)))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, SleepUntil(ctx, time.Now().Add(time.Hour)), context.Canceled)
}
