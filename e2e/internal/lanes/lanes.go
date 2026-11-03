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

	RestartsServices bool
	// LocalImages marks a lane whose network needs images the nightly does
	// not build, so the nightly and run-all leave it out. The scripts
	// directory builds them from local checkouts, remote-signer-nightly.yml
	// builds them in CI, and e2e/README.md says how to run each such lane.
	LocalImages bool
}

type SuiteID string

const (
	suiteExecutionABI              SuiteID = "execution-abi"
	suiteExecutionConsole          SuiteID = "execution-console"
	suiteExecutionTxpool           SuiteID = "execution-txpool"
	suiteExecutionRemoteSigner     SuiteID = "execution-remote-signer"
	suiteConsensusStakingAutomated SuiteID = "consensus-staking-automated"
	suiteConsensusStakingOperator  SuiteID = "consensus-staking-operator"
	suiteConsensusRemoteSigner     SuiteID = "consensus-remote-signer"
	suiteResilienceNetworkRestart  SuiteID = "resilience-network-restart"
	suiteSyncGenesis               SuiteID = "sync-genesis"
)

var suitePackages = map[SuiteID]string{
	suiteExecutionABI:              "./e2e/suites/execution/abi",
	suiteExecutionConsole:          "./e2e/suites/execution/console",
	suiteExecutionTxpool:           "./e2e/suites/execution/txpool",
	suiteExecutionRemoteSigner:     "./e2e/suites/execution/remotesigner",
	suiteConsensusStakingAutomated: "./e2e/suites/consensus/stakingautomated",
	suiteConsensusStakingOperator:  "./e2e/suites/consensus/stakingoperator",
	suiteConsensusRemoteSigner:     "./e2e/suites/consensus/remotesigner",
	suiteResilienceNetworkRestart:  "./e2e/suites/resilience/networkrestart",
	suiteSyncGenesis:               "./e2e/suites/sync/genesis",
}

var registry = []Lane{
	{
		Name:    "execution",
		Profile: devnet.ProfileSingle,
		Suites:  []SuiteID{suiteExecutionABI, suiteExecutionConsole, suiteExecutionTxpool},
		Timeout: 20 * time.Minute,
	},
	{
		// On the single profile a voting period is 512 five-second slots, so
		// the deposit counts after about 65 minutes, before activation,
		// attestation and exit.
		Name:    "consensus-staking-automated",
		Profile: devnet.ProfileSingle,
		Suites:  []SuiteID{suiteConsensusStakingAutomated},
		Timeout: 95 * time.Minute,
	},
	{
		// The deposit counts after about 65 minutes on the single profile,
		// before activation, attestation and exit.
		Name:    "consensus-staking-operator",
		Profile: devnet.ProfileSingle,
		Suites:  []SuiteID{suiteConsensusStakingOperator},
		Timeout: 95 * time.Minute,
	},
	{
		// Finality from about epoch 4 of eight five-second slots comes
		// first, then the refused requests and two more finalized epochs,
		// then a key removed and added again through the remote keys API,
		// which takes about seven epochs, and last the exit the signer
		// refuses. The signer is go-remotesigner, and the validator client
		// a qrysm with its remote signer client: neither image is published
		// yet. The pinned qrl-package does not run go-remotesigner either,
		// so the network is started from a qrl-package checkout that does.
		Name:        "consensus-remote-signer",
		Profile:     devnet.ProfileRemoteSigner,
		Suites:      []SuiteID{suiteConsensusRemoteSigner},
		Timeout:     35 * time.Minute,
		LocalImages: true,
	},
	{
		// The same suite, with the signer in token+mtls mode: the validator
		// client also presents a client certificate. No qrl-package issues
		// one yet (e2e/README.md says what it needs).
		Name:        "consensus-remote-signer-mtls",
		Profile:     devnet.ProfileRemoteSignerMTLS,
		Suites:      []SuiteID{suiteConsensusRemoteSigner},
		Timeout:     35 * time.Minute,
		LocalImages: true,
	},
	{
		// The same suite, with the signer signing voluntary exits: the last
		// spec exits a validator through it instead of being refused.
		Name:        "consensus-remote-signer-exits",
		Profile:     devnet.ProfileRemoteSignerExits,
		Suites:      []SuiteID{suiteConsensusRemoteSigner},
		Timeout:     35 * time.Minute,
		LocalImages: true,
	},
	{
		// go-remotesigner in execution mode in front of the execution
		// client: two transfers mined, a few signed messages and refused
		// requests, a few minutes in all. The signer is go-remotesigner's
		// full flavour, which is not published, and the pinned qrl-package
		// does not run its execution mode, so the network is started from a
		// qrl-package checkout that does.
		Name:        "execution-remote-signer",
		Profile:     devnet.ProfileRemoteSignerExecution,
		Suites:      []SuiteID{suiteExecutionRemoteSigner},
		Timeout:     15 * time.Minute,
		LocalImages: true,
	},
	{
		// Finality, the outage and finality again take about a dozen
		// 40-second epochs.
		Name:             "resilience-network-restart",
		Profile:          devnet.ProfileAnchored,
		Suites:           []SuiteID{suiteResilienceNetworkRestart},
		Timeout:          30 * time.Minute,
		RestartsServices: true,
	},
	{
		// About 200 blocks of history and their finality come first, at
		// five-second slots. Then a full-mode node syncs, stops for two epochs
		// past a new finalized checkpoint, and catches up.
		Name:    "sync",
		Profile: devnet.ProfileSingle,
		Suites:  []SuiteID{suiteSyncGenesis},
		Timeout: 60 * time.Minute,
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
	return slices.Contains(lane.Suites, suiteExecutionConsole) ||
		slices.Contains(lane.Suites, suiteSyncGenesis)
}

func (lane Lane) NeedsConsensusImage() bool {
	return slices.Contains(lane.Suites, suiteSyncGenesis)
}

func (lane Lane) NeedsValidatorImage() bool {
	return slices.Contains(lane.Suites, suiteConsensusStakingAutomated) ||
		slices.Contains(lane.Suites, suiteConsensusStakingOperator)
}

func (lane Lane) NeedsQrysmAlltoolsImage() bool {
	return slices.Contains(lane.Suites, suiteConsensusStakingOperator)
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
