// Package lanes defines the live E2E execution matrix.
package lanes

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
)

type Lane struct {
	Name    string
	Profile devnet.Profile
	Suites  []SuiteID
	Timeout time.Duration
}

type SuiteID string

const (
	suiteExecutionABI     SuiteID = "execution-abi"
	suiteExecutionConsole SuiteID = "execution-console"
	suiteSoakNetwork      SuiteID = "soak-network"
)

var suitePackages = map[SuiteID]string{
	suiteExecutionABI:     "./e2e/suites/execution/abi",
	suiteExecutionConsole: "./e2e/suites/execution/console",
	suiteSoakNetwork:      "./e2e/suites/perf/soak",
}

var registry = []Lane{
	{
		Name:    "execution",
		Profile: devnet.ProfileSingle,
		Suites:  []SuiteID{suiteExecutionABI, suiteExecutionConsole},
		Timeout: 30 * time.Minute,
	},
	{
		Name:    "soak",
		Profile: devnet.ProfileSoak,
		Suites:  []SuiteID{suiteSoakNetwork},
		// Covers an 8 h soak plus warm-up, cool-down and report slack.
		Timeout: 8*time.Hour + 45*time.Minute,
	},
}

func (id SuiteID) Package() string {
	return suitePackages[id]
}

func All() []Lane {
	return slices.Clone(registry)
}

// WithSuites returns the lane narrowed to the named suites; nil keeps them all.
func (lane Lane) WithSuites(names []string) (Lane, error) {
	if len(names) == 0 {
		return lane, nil
	}

	requested := make([]SuiteID, 0, len(names))
	for _, name := range names {
		id := SuiteID(strings.TrimSpace(name))
		if _, exists := suitePackages[id]; !exists {
			return Lane{}, fmt.Errorf("unknown E2E suite %q", name)
		}
		if !slices.Contains(lane.Suites, id) {
			return Lane{}, fmt.Errorf("suite %q is not available in lane %q", name, lane.Name)
		}
		if !slices.Contains(requested, id) {
			requested = append(requested, id)
		}
	}

	// Filter the lane's own list so the selection keeps the registered
	// execution order regardless of flag order.
	selected := make([]SuiteID, 0, len(requested))
	for _, id := range lane.Suites {
		if slices.Contains(requested, id) {
			selected = append(selected, id)
		}
	}
	lane.Suites = selected
	return lane, nil
}

func (lane Lane) Packages() []string {
	result := make([]string, len(lane.Suites))
	for index, id := range lane.Suites {
		result[index] = id.Package()
	}
	return result
}

func (lane Lane) NeedsExecutionImage() bool {
	return slices.Contains(lane.Suites, suiteExecutionConsole)
}

// StartTimeout is the network start budget for the lane when the caller
// did not set a longer one. Soak enclaves need time for work-node scale-up.
func (lane Lane) StartTimeout() time.Duration {
	if lane.Name == "soak" {
		return 20 * time.Minute
	}
	return 0
}

func RegisteredSuites() []SuiteID {
	return slices.Sorted(maps.Keys(suitePackages))
}

func Named(name string) (Lane, error) {
	for _, lane := range registry {
		if lane.Name == name {
			return lane, nil
		}
	}
	return Lane{}, fmt.Errorf("unknown E2E lane %q", name)
}
