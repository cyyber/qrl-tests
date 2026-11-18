package health

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/p2p/qnode"
)

func TestExcept(t *testing.T) {
	own, other := qnode.ID{1}, qnode.ID{2}
	drops := []PeerDrop{{Peer: own}, {Peer: other}, {Peer: own}}
	require.Equal(t, []PeerDrop{{Peer: other}}, except(drops, own))
	require.Len(t, drops, 3, "the drops passed in are not changed")
}
