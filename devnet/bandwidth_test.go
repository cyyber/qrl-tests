package devnet

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLimitRules(t *testing.T) {
	peers := []netip.Addr{netip.MustParseAddr("172.16.0.5"), netip.MustParseAddr("172.16.0.6")}
	rules := limitRules("10mbit", peers)
	require.True(t, strings.HasPrefix(rules, "set -e\n"), "the script stops at the first rule it cannot add")
	require.Contains(t, rules, "tbf rate 10mbit")
	for _, peer := range peers {
		require.Contains(t, rules, "match ip dst "+peer.String()+"/32 flowid 1:4", "traffic to the peer is shaped")
		require.Contains(t, rules, "match ip src "+peer.String()+"/32 action police rate 10mbit", "traffic from the peer is policed")
	}
	require.Equal(t, 2, strings.Count(rules, "flowid 1:4"), "only the peers' traffic is limited")
	require.Contains(t, unlimitRules, "|| true", "removing the limit tolerates a rule that is not there")
}
