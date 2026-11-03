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

	profile, err = ParseProfile("anchored")
	require.NoError(t, err)
	require.Equal(t, ProfileAnchored, profile)

	profile, err = ParseProfile("spare-validator")
	require.NoError(t, err)
	require.Equal(t, ProfileSpareValidator, profile)

	_, err = ParseProfile("unknown")
	require.ErrorContains(t, err, "unknown development-network profile")
}

func TestSpareValidators(t *testing.T) {
	require.Empty(t, ProfileSingle.SpareValidators())
	require.Empty(t, ProfileAnchored.SpareValidators())
	require.Equal(t, []uint64{64}, ProfileSpareValidator.SpareValidators())
}
