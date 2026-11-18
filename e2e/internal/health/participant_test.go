package health

import (
	"context"
	"io"
	"strings"
	"testing"

	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

type fakeStats string

func (stats fakeStats) ContainerStats(_ context.Context, _ string, _ dockerclient.ContainerStatsOptions) (dockerclient.ContainerStatsResult, error) {
	return dockerclient.ContainerStatsResult{Body: io.NopCloser(strings.NewReader(string(stats)))}, nil
}

func TestMemoryUsage(t *testing.T) {
	usage, err := MemoryUsage(t.Context(), fakeStats(`{"memory_stats":{"usage":123456789,"limit":1}}`), "node")
	require.NoError(t, err)
	require.Equal(t, uint64(123456789), usage)

	usage, err = MemoryUsage(t.Context(), fakeStats(`{"memory_stats":{"usage":1000,"stats":{"inactive_file":300}}}`), "node")
	require.NoError(t, err)
	require.Equal(t, uint64(700), usage, "cgroup v2, without inactive files")
	usage, err = MemoryUsage(t.Context(), fakeStats(`{"memory_stats":{"usage":1000,"stats":{"total_inactive_file":400}}}`), "node")
	require.NoError(t, err)
	require.Equal(t, uint64(600), usage, "cgroup v1, without inactive files")

	_, err = MemoryUsage(t.Context(), fakeStats(`{}`), "node")
	require.ErrorContains(t, err, "no memory usage")
}
