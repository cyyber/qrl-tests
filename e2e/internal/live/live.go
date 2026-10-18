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

// StopValidators stops every validator client, so no block or attestation
// is signed after it returns.
func (runtime *Runtime) StopValidators(ctx context.Context) error {
	validators, _, _ := runtime.services()
	return devnet.NewManager().StopServices(ctx, runtime.environment.EnclaveName, validators)
}

// StopNodes stops every beacon node, then every execution client. They keep
// their data.
func (runtime *Runtime) StopNodes(ctx context.Context) error {
	_, beacons, executions := runtime.services()
	return devnet.NewManager().StopServices(ctx, runtime.environment.EnclaveName, slices.Concat(beacons, executions))
}

// KillParticipants kills every validator client, beacon node and execution
// client without letting them shut down, as a power loss would. They keep
// whatever they had written to disk.
func (runtime *Runtime) KillParticipants(ctx context.Context) error {
	validators, beacons, executions := runtime.services()
	return devnet.NewManager().KillServices(
		ctx, runtime.environment.EnclaveName, slices.Concat(validators, beacons, executions),
	)
}

// StartParticipants starts the services taken down by StopValidators and
// StopNodes or by KillParticipants, in the order of a full-outage recovery:
// every execution client, the beacon nodes from the first participant on,
// then every validator client. A started service is published on other host
// ports, so the environment is read again and the nodes opened before the
// restart are closed. Only this runtime sees the new ports: the lane manifest
// keeps the old ones, so a lane that restarts services runs a single suite.
func (runtime *Runtime) StartParticipants(ctx context.Context) error {
	validators, beacons, executions := runtime.services()
	services := slices.Concat(executions, beacons, validators)
	manager := devnet.NewManager()
	name := runtime.environment.EnclaveName
	if err := manager.StartServices(ctx, name, services); err != nil {
		return err
	}

	environment, err := manager.Inspect(ctx, name)
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
