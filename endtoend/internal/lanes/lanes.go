// Package lanes defines the live E2E execution matrix.
package lanes

import (
	"fmt"
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

type Suite struct {
	ID      SuiteID
	Package string
}

var suites = map[SuiteID]Suite{
	SuiteExecutionABI: {ID: SuiteExecutionABI, Package: "./endtoend/suites/execution/abi"},
}

var registry = []Lane{
	{
		Name:    "execution-abi",
		Profile: devnet.ProfileSingle,
		Suites:  []SuiteID{SuiteExecutionABI},
		Timeout: 90 * time.Minute,
	},
}

func All() []Lane {
	return slices.Clone(registry)
}

func (lane Lane) Select(names []string) (Lane, error) {
	if len(names) == 0 {
		return lane, nil
	}
	wanted := make(map[SuiteID]struct{}, len(names))
	for _, name := range names {
		id := SuiteID(strings.TrimSpace(name))
		if _, exists := suites[id]; !exists {
			return Lane{}, fmt.Errorf("unknown E2E suite %q", name)
		}
		wanted[id] = struct{}{}
	}
	selected := make([]SuiteID, 0, len(wanted))
	for _, id := range lane.Suites {
		if _, exists := wanted[id]; exists {
			selected = append(selected, id)
			delete(wanted, id)
		}
	}
	if len(wanted) != 0 {
		missing := make([]string, 0, len(wanted))
		for id := range wanted {
			missing = append(missing, string(id))
		}
		slices.Sort(missing)
		return Lane{}, fmt.Errorf("suites %s are not available in lane %q", strings.Join(missing, ", "), lane.Name)
	}
	lane.Suites = selected
	return lane, nil
}

func (lane Lane) Packages() []string {
	result := make([]string, len(lane.Suites))
	for index, id := range lane.Suites {
		result[index] = suites[id].Package
	}
	return result
}

func RegisteredSuites() []Suite {
	result := make([]Suite, 0, len(suites))
	for _, suite := range suites {
		result = append(result, suite)
	}
	slices.SortFunc(result, func(left, right Suite) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	return result
}

func Named(name string) (Lane, error) {
	for _, lane := range registry {
		if lane.Name == name {
			return lane, nil
		}
	}
	return Lane{}, fmt.Errorf("unknown E2E lane %q", name)
}
