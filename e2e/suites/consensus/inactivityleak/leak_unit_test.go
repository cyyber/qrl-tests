package inactivityleak

import (
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/stretchr/testify/require"
)

const shorPerQRL = 1_000_000_000

func TestMoreThanAThird(t *testing.T) {
	// Half of 64 validators.
	require.True(t, moreThanAThird(32*40_000*shorPerQRL, 64*40_000*shorPerQRL))
	// Exactly a third leaves the rest with two thirds, enough to justify.
	require.False(t, moreThanAThird(30*shorPerQRL, 90*shorPerQRL))
	require.True(t, moreThanAThird(30*shorPerQRL+1, 90*shorPerQRL))
}

func TestTimelyTarget(t *testing.T) {
	require.True(t, timelyTarget(beacon.AttestationReward{Target: 652_665_000}))
	// A leak pays a timely target nothing.
	require.True(t, timelyTarget(beacon.AttestationReward{}))
	require.False(t, timelyTarget(beacon.AttestationReward{Target: -652_665_000}))
}
