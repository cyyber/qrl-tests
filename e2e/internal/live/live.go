// Package live opens the shared clients and wallet used by live E2E suites.
package live

import (
	"context"
	"fmt"
	"math/big"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/manifest"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorclient"
	"github.com/cyyber/qrl-tests/internal/devwallet"
	"github.com/theQRL/go-qrl/common"
	qrlwallet "github.com/theQRL/go-qrl/crypto/pqcrypto/wallet"
	"github.com/theQRL/go-qrl/qrlclient"
)

// Runtime owns the network metadata and shared resources for one live suite.
type Runtime struct {
	Wallet         qrlwallet.Wallet
	Address        common.Address
	ChainID        *big.Int
	ExecutionImage string
	ValidatorImage string

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
	Validator             *validatorclient.Client
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
		Wallet:         wallet,
		Address:        common.Address(wallet.GetAddress()),
		ExecutionImage: suiteManifest.ExecutionImage,
		ValidatorImage: suiteManifest.ValidatorImage,
		environment:    suiteManifest.Environment,
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
