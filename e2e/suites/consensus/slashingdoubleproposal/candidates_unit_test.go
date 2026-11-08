package slashingdoubleproposal

import (
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/stretchr/testify/require"
)

func TestProposalCandidates(t *testing.T) {
	duty := func(slot, index uint64) beacon.ProposerDuty {
		return beacon.ProposerDuty{Slot: slot, ValidatorIndex: index}
	}
	current := []beacon.ProposerDuty{duty(16, 3), duty(17, 40), duty(18, 9)}
	next := []beacon.ProposerDuty{duty(24, 1), duty(25, 9), duty(26, 33), duty(27, 2), duty(28, 1), duty(29, 5)}
	// Node 1 runs 0 to 31; validator 5 has already been duplicated.
	candidates := []uint64{1, 2, 3, 5, 9}

	// 1 proposes twice and 9 proposed in the current epoch, so another
	// proposal of theirs would be a second offense; 33 is not node 1's.
	require.Equal(t, []beacon.ProposerDuty{duty(27, 2)}, proposalCandidates(current, next, candidates, []uint64{5}))
	require.Equal(t, []beacon.ProposerDuty{duty(29, 5), duty(27, 2)}, proposalCandidates(current, next, candidates, nil))
	require.Empty(t, proposalCandidates(current, nil, candidates, nil))
}
