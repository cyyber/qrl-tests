// Package live opens the shared clients and wallet used by live E2E suites.
package live

import (
	"context"
	"fmt"
	"math/big"
	"slices"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/manifest"
	"github.com/cyyber/qrl-tests/internal/devwallet"
	"github.com/theQRL/go-qrl/common"
	qrlwallet "github.com/theQRL/go-qrl/crypto/pqcrypto/wallet"
	"github.com/theQRL/go-qrl/qrlclient"
)

// Runtime owns the network metadata and shared resources for one live suite.
type Runtime struct {
	Wallet             qrlwallet.Wallet
	Address            common.Address
	ChainID            *big.Int
	ExecutionImage     string
	ValidatorImage     string
	QrysmAlltoolsImage string

	environment devnet.Environment
	nodes       []*Node
	// stopped holds the services stopped and not yet started again.
	stopped []string
}

// Node is an open handle to one network participant: its execution and
// beacon clients plus the shared suite Runtime.
type Node struct {
	*Runtime
	ExecutionRPCURL       string
	ExecutionWebSocketURL string
	Execution             *qrlclient.Client
	ExecutionWebSocket    *qrlclient.Client
	BeaconURL             string
	BeaconGRPC            string
	ConsensusServiceID    string
	Beacon                *beacon.Client
}

// Load resolves the configured test environment and restores the disposable
// development wallet once for the suite.
func Load() (*Runtime, error) {
	suiteManifest, err := manifest.FromEnv()
	if err != nil {
		return nil, err
	}

	wallet, err := devwallet.Restore()
	if err != nil {
		return nil, err
	}

	runtime := &Runtime{
		Wallet:             wallet,
		Address:            common.Address(wallet.GetAddress()),
		ExecutionImage:     suiteManifest.ExecutionImage,
		ValidatorImage:     suiteManifest.ValidatorImage,
		QrysmAlltoolsImage: suiteManifest.QrysmAlltoolsImage,
		environment:        suiteManifest.Environment,
	}
	return runtime, nil
}

func (runtime *Runtime) PrimaryNode(ctx context.Context) (*Node, error) {
	participant, err := runtime.environment.Primary()
	if err != nil {
		return nil, err
	}
	return runtime.open(ctx, participant, false)
}

// Nodes opens every participant, lowest index first.
func (runtime *Runtime) Nodes(ctx context.Context) ([]*Node, error) {
	nodes := make([]*Node, len(runtime.environment.Participants))
	for index, participant := range runtime.environment.Participants {
		node, err := runtime.open(ctx, participant, false)
		if err != nil {
			return nil, err
		}
		nodes[index] = node
	}
	return nodes, nil
}

func (runtime *Runtime) PrimaryWithWebSocket(ctx context.Context) (*Node, error) {
	participant, err := runtime.environment.Primary()
	if err != nil {
		return nil, err
	}
	return runtime.open(ctx, participant, true)
}

func (runtime *Runtime) open(ctx context.Context, participant devnet.Participant, withWebSocket bool) (*Node, error) {
	client, err := qrlclient.DialContext(ctx, participant.Execution.RPCURL)
	if err != nil {
		return nil, fmt.Errorf("open participant %d HTTP RPC: %w", participant.Index, err)
	}

	if runtime.ChainID == nil {
		runtime.ChainID, err = client.ChainID(ctx)
		if err != nil {
			client.Close()
			return nil, fmt.Errorf("read participant %d chain ID: %w", participant.Index, err)
		}
	}

	beaconClient, err := beacon.New(participant.Consensus.URL)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("open participant %d beacon API: %w", participant.Index, err)
	}

	node := &Node{
		Runtime:               runtime,
		ExecutionRPCURL:       participant.Execution.RPCURL,
		ExecutionWebSocketURL: participant.Execution.WebSocketURL,
		Execution:             client,
		BeaconURL:             participant.Consensus.URL,
		BeaconGRPC:            participant.Consensus.GRPC,
		ConsensusServiceID:    participant.Consensus.ID,
		Beacon:                beaconClient,
	}
	if withWebSocket {
		node.ExecutionWebSocket, err = qrlclient.DialContext(ctx, participant.Execution.WebSocketURL)
		if err != nil {
			node.Close()
			return nil, fmt.Errorf("open participant %d WebSocket RPC: %w", participant.Index, err)
		}
	}

	runtime.nodes = append(runtime.nodes, node)
	return node, nil
}

// CanRestartParticipants reports whether the participants keep their data
// across StopValidators, StopNodes and StartParticipants. Kurtosis keeps a
// stopped service's container only on Docker; on Kubernetes it deletes the
// pod, and the restarted nodes start empty.
func (runtime *Runtime) CanRestartParticipants() error {
	if backend := runtime.environment.Backend; backend != devnet.BackendDocker {
		return fmt.Errorf("restarting participants needs the %s backend, not %q", devnet.BackendDocker, backend)
	}
	return nil
}

// StopValidators stops every validator client, so no block is proposed
// after it returns.
func (runtime *Runtime) StopValidators(ctx context.Context) error {
	validators, _, _ := runtime.services()
	return runtime.stopServices(ctx, validators)
}

// StopNodes stops every beacon node, then every execution client. They keep
// their data.
func (runtime *Runtime) StopNodes(ctx context.Context) error {
	_, beacons, executions := runtime.services()
	return runtime.stopServices(ctx, slices.Concat(beacons, executions))
}

func (runtime *Runtime) stopServices(ctx context.Context, services []string) error {
	if err := runtime.CanRestartParticipants(); err != nil {
		return err
	}
	// Recorded first: a failed stop may still have stopped some of them.
	runtime.stopped = append(runtime.stopped, services...)
	return devnet.NewManager().StopServices(ctx, runtime.environment.EnclaveName, services)
}

// HasStoppedParticipants reports whether a service stopped through the
// runtime has not been started again.
func (runtime *Runtime) HasStoppedParticipants() bool {
	return len(runtime.stopped) > 0
}

// StartParticipants starts the services that StopValidators and StopNodes
// stopped, in the order of a full-outage recovery: every execution client,
// the beacon nodes from the first participant on, then every validator
// client. A started service is published on other host ports, so the
// environment is read again and the nodes opened before the restart are
// closed. Only this runtime sees the new ports: the lane manifest keeps the
// old ones, so a lane that restarts services runs a single suite.
func (runtime *Runtime) StartParticipants(ctx context.Context) error {
	validators, beacons, executions := runtime.services()
	services := slices.DeleteFunc(slices.Concat(executions, beacons, validators), func(name string) bool {
		return !slices.Contains(runtime.stopped, name)
	})
	if len(services) == 0 {
		return nil
	}
	manager := devnet.NewManager()
	name := runtime.environment.EnclaveName
	if err := manager.StartServices(ctx, name, services); err != nil {
		return err
	}
	runtime.stopped = nil

	environment, err := manager.Environment(ctx, name)
	if err != nil {
		return err
	}
	environment.Backend = runtime.environment.Backend
	runtime.Close()
	runtime.environment = environment
	return nil
}

// services lists the participants' services by kind, lowest participant
// first, without the ones a participant does not run.
func (runtime *Runtime) services() (validators, beacons, executions []string) {
	for _, participant := range runtime.environment.Participants {
		validators = append(validators, participant.Validator.Name)
		beacons = append(beacons, participant.Consensus.Name)
		executions = append(executions, participant.Execution.Name)
	}
	named := func(names []string) []string {
		return slices.DeleteFunc(names, func(name string) bool { return name == "" })
	}
	return named(validators), named(beacons), named(executions)
}

func (runtime *Runtime) Close() {
	for _, node := range runtime.nodes {
		node.Close()
	}
	runtime.nodes = nil
}

func (node *Node) Close() {
	if node.ExecutionWebSocket != nil {
		node.ExecutionWebSocket.Close()
	}
	if node.Execution != nil {
		node.Execution.Close()
	}
}
