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

	profile, err = ParseProfile("soak")
	require.NoError(t, err)
	require.Equal(t, ProfileSoak, profile)
	require.Equal(t, soakParticipants, profile.ParticipantCount())
	require.Equal(t, 1, ProfileSingle.ParticipantCount())

	_, err = ParseProfile("unknown")
	require.ErrorContains(t, err, "unknown development-network profile")
}
