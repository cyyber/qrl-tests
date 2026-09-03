package soak

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLiveAnnotations(t *testing.T) {
	sample := Sample{
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
	annotations := LiveAnnotations(sample, 12)
	require.Equal(t, "steady", annotations["qrl.io/phase"])
	require.Equal(t, "12", annotations["qrl.io/samples"])
	require.Equal(t, "2", annotations["qrl.io/finalized-epoch"])
	require.Equal(t, "40", annotations["qrl.io/head-slot"])
	require.Equal(t, "5", annotations["qrl.io/tx-per-slot"])
	require.Equal(t, "512", annotations["qrl.io/el-rss-mb"])
}
