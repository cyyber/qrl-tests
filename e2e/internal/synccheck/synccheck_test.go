package synccheck

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSampledRange(t *testing.T) {
	require.Nil(t, sampledRange(5, 4))
	require.Equal(t, []uint64{7}, sampledRange(7, 7))
	// A short range keeps every value.
	require.Equal(t, []uint64{2, 3, 4, 5}, sampledRange(2, 5))

	// A long one spreads about comparedSamples values and keeps both ends.
	values := sampledRange(100, 100+50*comparedSamples)
	require.Equal(t, uint64(100), values[0])
	require.Equal(t, uint64(100+50*comparedSamples), values[len(values)-1])
	require.InDelta(t, comparedSamples, len(values), 1)
	for index := 1; index < len(values); index++ {
		require.Greater(t, values[index], values[index-1])
	}
}
