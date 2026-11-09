package doubleproposal

import (
	"cmp"
	"slices"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
)

// proposalCandidates returns the next epoch's proposals the suite can
// duplicate, latest first.
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
