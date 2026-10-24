package soak

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProgressLine(t *testing.T) {
	sample := Sample{
		At:    time.Date(2026, 10, 25, 3, 4, 5, 0, time.UTC),
		Phase: PhaseSteady,
		Participants: []ParticipantSample{
			{
				FinalizedEpoch: 3, HeadSlot: 40, TxInHead: 4,
				Clients: map[Client]ClientMetrics{
					ClientExecution: {RSSBytes: 512 << 20, Scraped: true},
				},
			},
			{
				FinalizedEpoch: 2, HeadSlot: 38, TxInHead: 6,
				Clients: map[Client]ClientMetrics{
					ClientExecution: {RSSBytes: 256 << 20, Scraped: true},
				},
			},
		},
	}
	require.Equal(t, "03:04:05Z phase=steady samples=12 finalized=2 head_slot=40 tx/slot=5 el_rss=512MB", ProgressLine(sample, 12))

	var out bytes.Buffer
	require.NoError(t, LogProgress{Out: &out}.Report(t.Context(), sample, 12))
	require.Equal(t, ProgressLine(sample, 12)+"\n", out.String())
}
