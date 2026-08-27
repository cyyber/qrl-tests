package devnet

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseProfile(t *testing.T) {
	profile, err := ParseProfile("")
	require.NoError(t, err)
	require.Equal(t, ProfileSingle, profile)

	profile, err = ParseProfile("single")
	require.NoError(t, err)
	require.Equal(t, ProfileSingle, profile)

	profile, err = ParseProfile("  single  ")
	require.NoError(t, err)
	require.Equal(t, ProfileSingle, profile)

	profile, err = ParseProfile("   ")
	require.NoError(t, err)
	require.Equal(t, ProfileSingle, profile)

	_, err = ParseProfile("unknown")
	require.ErrorContains(t, err, "unknown development-network profile")
}

func TestProfileExpectations(t *testing.T) {
	expectations, found := ProfileSingle.Expectations()
	require.True(t, found)
	require.Equal(t, NetworkExpectations{
		ChainID:            "0x539",
		NetworkID:          "1337",
		ExecutionPeerCount: 0,
	}, expectations)

	_, found = Profile("unknown").Expectations()
	require.False(t, found)
}
