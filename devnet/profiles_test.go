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

	profile, err = ParseProfile("duplicate-validator")
	require.NoError(t, err)
	require.Equal(t, ProfileDuplicateValidator, profile)

	profile, err = ParseProfile("mainnet-slot-time")
	require.NoError(t, err)
	require.Equal(t, ProfileMainnetSlotTime, profile)

	profile, err = ParseProfile("six-second-slots")
	require.NoError(t, err)
	require.Equal(t, ProfileSixSecondSlots, profile)

	_, err = ParseProfile("unknown")
	require.ErrorContains(t, err, "unknown development-network profile")
}

func TestSpareValidators(t *testing.T) {
	require.Empty(t, ProfileSingle.SpareValidators())
	require.Empty(t, ProfileAnchored.SpareValidators())
	require.Equal(t, []uint64{64}, ProfileSpareValidator.SpareValidators())
	require.Equal(t, []uint64{64}, ProfileDuplicateValidator.SpareValidators())
}

func TestParticipantValidators(t *testing.T) {
	second := ProfileDuplicateValidator.ParticipantValidators(1)
	require.Len(t, second, 32)
	require.Equal(t, uint64(32), second[0])
	require.Equal(t, uint64(63), second[31])
	require.Equal(t, second, ProfileDoubleProposal.ParticipantValidators(1))
	require.Empty(t, ProfileDoubleProposal.ParticipantValidators(2))
	require.Empty(t, ProfileDoubleProposal.SpareValidators())
	require.Len(t, ProfileInactivityLeak.ParticipantValidators(0), 43)
	stopped := ProfileInactivityLeak.ParticipantValidators(1)
	require.Len(t, stopped, 22)
	require.Equal(t, uint64(43), stopped[0])
	require.Equal(t, uint64(64), stopped[21])
	require.Empty(t, ProfileInactivityLeak.ParticipantValidators(2))
	require.Empty(t, ProfileInactivityLeak.SpareValidators())
	require.Len(t, ProfileMainnetSlotTime.ParticipantValidators(0), 256)
	require.Empty(t, ProfileMainnetSlotTime.SpareValidators())
	require.Len(t, ProfileSixSecondSlots.ParticipantValidators(0), 64)
	require.Empty(t, ProfileSixSecondSlots.SpareValidators())
	require.Len(t, ProfileSingle.ParticipantValidators(0), 64)
	require.Empty(t, ProfileSingle.ParticipantValidators(1))
	require.Empty(t, ProfileSingle.ParticipantValidators(-1))
}
