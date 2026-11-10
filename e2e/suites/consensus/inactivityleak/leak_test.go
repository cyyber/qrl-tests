package inactivityleak

import "github.com/cyyber/qrl-tests/e2e/internal/beacon"

// moreThanAThird reports whether stake is more than a third of total.
func moreThanAThird(stake, total uint64) bool {
	return 3*stake > total
}

// timelyTarget reports whether the target vote was timely: only a missed one
// is penalized.
func timelyTarget(reward beacon.AttestationReward) bool {
	return reward.Target >= 0
}
