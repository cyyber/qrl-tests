package inactivityleak

import "github.com/cyyber/qrl-tests/e2e/internal/beacon"

// moreThanAThird reports whether stake is more than a third of total: enough
// that without it, the rest falls short of the two thirds that justify.
func moreThanAThird(stake, total uint64) bool {
	return 3*stake > total
}

// timelyTarget reports whether a validator's target vote was timely, from its
// reward: a missed target always costs a penalty, a timely one never does.
func timelyTarget(reward beacon.AttestationReward) bool {
	return reward.Target >= 0
}
