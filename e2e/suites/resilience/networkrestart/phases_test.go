package networkrestart

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
)

// KillPhasesEnv names the kill phases a run executes, as a comma-separated
// list of phase keys or 1-based positions in killPhases. Unset or empty, the
// run executes every phase. A run that measures one phase on a fresh network
// sets it; the nightly leaves it unset.
const KillPhasesEnv = "RESILIENCE_KILL_PHASES"

// killPhase is the point within a slot at which every node is killed. A
// validator proposes at the start of its slot, attests a third of the way in
// and aggregates two thirds in, and at the end of the last slot of an epoch
// the beacon node runs the epoch transition, so what each node has on disk
// and in flight depends on where the kill lands.
type killPhase struct {
	// key names the phase in KillPhasesEnv.
	key  string
	name string
	// The offset into the slot, as the fraction numerator/denominator.
	numerator, denominator int64
	// lastSlotOfEpoch kills in the last slot of an epoch only.
	lastSlotOfEpoch bool
}

var killPhases = []killPhase{
	{key: "slot-start", name: "at the start of a slot, while the block is proposed", numerator: 0, denominator: 1},
	{key: "slot-third", name: "a third into a slot, while attestations are made", numerator: 1, denominator: 3},
	{key: "slot-two-thirds", name: "two thirds into a slot, while attestations are aggregated", numerator: 2, denominator: 3},
	// Nine tenths in, so that the kills, which land up to 0.3 s after they
	// are sent on Docker Desktop, still fall in the slot. The restart then
	// loads the state the epoch transition was about to save.
	{
		key: "epoch-end",
		name: "at the end of an epoch's last slot, while the epoch transition with its justification " +
			"and finalization updates and the state save a restart loads from are in flight",
		numerator: 9, denominator: 10, lastSlotOfEpoch: true,
	},
}

func (phase killPhase) offset(chain chaininfo.Info) time.Duration {
	return time.Duration(phase.numerator) * chain.SlotDuration / time.Duration(phase.denominator)
}

func (phase killPhase) slots(chain chaininfo.Info) chaininfo.SlotFilter {
	if phase.lastSlotOfEpoch {
		return chain.LastSlotOfEpoch
	}
	return chaininfo.AnySlot
}

// selectedKillPhases returns the phases KillPhasesEnv names, in the order of
// killPhases, or every phase when it is unset.
func selectedKillPhases() ([]killPhase, error) {
	return selectKillPhases(os.Getenv(KillPhasesEnv), killPhases)
}

// selectKillPhases picks from phases the ones the selection names, by key or
// 1-based position, in the order of phases. An empty selection keeps them
// all.
func selectKillPhases(selection string, phases []killPhase) ([]killPhase, error) {
	if strings.TrimSpace(selection) == "" {
		return phases, nil
	}

	wanted := make([]bool, len(phases))
	for _, item := range strings.Split(selection, ",") {
		item = strings.TrimSpace(item)
		index, err := strconv.Atoi(item)
		if err == nil {
			if index < 1 || index > len(phases) {
				return nil, fmt.Errorf("%s: phase %d is outside 1 to %d", KillPhasesEnv, index, len(phases))
			}
			wanted[index-1] = true
			continue
		}
		position := -1
		for candidate, phase := range phases {
			if phase.key == item {
				position = candidate
			}
		}
		if position < 0 {
			return nil, fmt.Errorf("%s: unknown phase %q, want one of %s", KillPhasesEnv, item, strings.Join(phaseKeys(phases), ", "))
		}
		wanted[position] = true
	}

	var selected []killPhase
	for index, phase := range phases {
		if wanted[index] {
			selected = append(selected, phase)
		}
	}
	return selected, nil
}

func phaseKeys(phases []killPhase) []string {
	keys := make([]string, len(phases))
	for index, phase := range phases {
		keys[index] = phase.key
	}
	return keys
}
