package slashingproposal

import (
	"cmp"
	"slices"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
)

// proposalCandidates returns the proposals of the next epoch that a duplicate
// of their proposer's key can turn into a double proposal and nothing else:
// the proposer is one of candidates, not one of excluded, and proposes no
// other slot in the current epoch or the next. The latest come first, to give
// the duplicate the most time to start.
func proposalCandidates(current, next []beacon.ProposerDuty, candidates, excluded []uint64) []beacon.ProposerDuty {
	proposals := make(map[uint64]int)
	for _, duty := range slices.Concat(current, next) {
		proposals[duty.ValidatorIndex]++
	}
	var chosen []beacon.ProposerDuty
	for _, duty := range next {
		if slices.Contains(candidates, duty.ValidatorIndex) && !slices.Contains(excluded, duty.ValidatorIndex) &&
			proposals[duty.ValidatorIndex] == 1 {
			chosen = append(chosen, duty)
		}
	}
	slices.SortFunc(chosen, func(first, second beacon.ProposerDuty) int { return cmp.Compare(second.Slot, first.Slot) })
	return chosen
}
