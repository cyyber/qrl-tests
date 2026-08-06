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
	SuiteExecutionABI SuiteID = "execution-abi"
)

var suitePackages = map[SuiteID]string{
	SuiteExecutionABI: "./e2e/suites/execution/abi",
}

var registry = []Lane{
	{
		Name:    "execution-abi",
		Profile: devnet.ProfileSingle,
		Suites:  []SuiteID{SuiteExecutionABI},
		Timeout: 15 * time.Minute,
	},
}

func (id SuiteID) Package() string {
	return suitePackages[id]
}

func All() []Lane {
	return slices.Clone(registry)
}

func (lane Lane) Select(names []string) (Lane, error) {
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
