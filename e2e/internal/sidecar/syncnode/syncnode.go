// Package syncnode runs a node without validators, a gqrl execution client and
// a Qrysm beacon node, that syncs from a devnet participant. Both containers
// join the Kurtosis enclave's Docker network: the node dials the source's
// clients at their private IPs and is never dialled back, and the beacon node
// reaches its own execution client by a network alias.
package syncnode

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/internal/dockerapi"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/p2p"
	"github.com/theQRL/go-qrl/qrlclient"
)

const (
	rpcPort    = 8545
	enginePort = 8551
	beaconPort = 3500

	networkConfigDir  = "/network-configs"
	genesisPath       = networkConfigDir + "/genesis.json"
	chainConfigPath   = networkConfigDir + "/config.yaml"
	genesisStatePath  = networkConfigDir + "/genesis.ssz"
	jwtPath           = "/jwt/jwtsecret"
	executionDataDir  = "/data/gqrl/execution-data"
	beaconDataDir     = "/data/qrysm/beacon-data"
	startScriptPath   = "/start-gqrl.sh"
	readyPollInterval = time.Second

	// Shutdown budgets before Docker kills a client. gqrl writes its state
	// to disk on the way out.
	executionStopTimeout = time.Minute
	beaconStopTimeout    = 30 * time.Second

	// failureLogLines is how much of each client's output a failed spec
	// reports.
	failureLogLines = 100
)

// startScript initializes the data directory once, so a restarted container
// keeps its chain, and reads its settings from the environment set by
// executionEnv.
const startScript = `#!/bin/sh
set -eu
if [ ! -d "${DATA_DIR}/gqrl/chaindata" ]; then
  gqrl init --datadir="${DATA_DIR}" "${GENESIS_FILE}"
fi
exec gqrl \
  --datadir="${DATA_DIR}" \
  --networkid="${NETWORK_ID}" \
  --syncmode="${SYNC_MODE}" \
  --cache=512 \
  --nodiscover \
  --http \
  --http.addr=0.0.0.0 \
  --http.port="${RPC_PORT}" \
  --http.vhosts='*' \
  --http.api=admin,qrl,net,web3,debug,txpool \
  --authrpc.addr=0.0.0.0 \
  --authrpc.port="${ENGINE_PORT}" \
  --authrpc.vhosts='*' \
  --authrpc.jwtsecret="${JWT_FILE}"
`

// Config describes the node Start launches.
type Config struct {
	// Name tells nodes apart in errors and in the enclave's DNS. It is a DNS
	// label: lowercase letters, digits and dashes.
	Name string
	// ExecutionImage and ConsensusImage are the devnet's gqrl and Qrysm
	// beacon images.
	ExecutionImage string
	ConsensusImage string
	// SyncMode is gqrl's --syncmode, "snap" or "full".
	SyncMode string
	// BeaconOrigin is where the beacon node starts syncing from: Genesis,
	// the default, or Checkpoint, the source's finalized checkpoint.
	BeaconOrigin BeaconOrigin
	// Source is the participant the node syncs from.
	Source devnet.Participant
}

// BeaconOrigin is where a beacon node starts syncing from.
type BeaconOrigin string

const (
	// Genesis replays every block from the genesis state.
	Genesis BeaconOrigin = "genesis"
	// Checkpoint downloads the source's finalized state and block over its
	// REST API and starts from there. Qrysm does not backfill, so the node
	// never holds the blocks before that checkpoint.
	Checkpoint BeaconOrigin = "checkpoint"
)

// Node is a running node that the caller must Close.
type Node struct {
	// RPCURL and BeaconURL are the clients' host-published endpoints.
	RPCURL    string
	BeaconURL string
	Execution *qrlclient.Client
	Beacon    *beacon.Client

	execution   *sidecar.Container
	consensus   *sidecar.Container
	sourceQnode string
	closeClient func() error
}

type dockerClient interface {
	sidecar.Client
	devnet.ServiceFileClient
}

// Start launches the node and waits until both clients answer and the
// execution client has the source as its peer. It does not wait for sync.
func Start(ctx context.Context, config Config) (*Node, error) {
	client, err := dockerapi.New()
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	node, err := start(ctx, config, client, querySource)
	if err != nil {
		return nil, errors.Join(err, client.Close())
	}
	node.closeClient = client.Close
	return node, nil
}

// source is what the node needs from the participant it syncs from.
type source struct {
	networkID string
	// qnode and beaconPeer are the source's execution and beacon addresses
	// on the enclave network.
	qnode      string
	beaconPeer string
	// beaconAPI is the source's beacon REST API on the enclave network,
	// where a checkpoint-synced node fetches its origin from.
	beaconAPI string
}

type sourceQuery func(context.Context, devnet.Participant) (source, error)

func start(ctx context.Context, config Config, client dockerClient, query sourceQuery) (*Node, error) {
	if err := validate(config); err != nil {
		return nil, err
	}
	peer, err := query(ctx, config.Source)
	if err != nil {
		return nil, fmt.Errorf("query source participant %d: %w", config.Source.Index, err)
	}
	networkName, err := devnet.ServiceNetwork(ctx, client, config.Source.Consensus.ServiceInfo)
	if err != nil {
		return nil, fmt.Errorf("find the enclave network: %w", err)
	}
	files, err := readNetworkFiles(ctx, client, config.Source)
	if err != nil {
		return nil, err
	}
	jwt, err := newJWTSecret()
	if err != nil {
		return nil, err
	}

	node := &Node{sourceQnode: peer.qnode}
	executionAlias := "qrl-tests-" + config.Name + "-execution"
	node.execution, err = sidecar.Start(ctx, client, sidecar.Spec{
		Name:       config.Name + " execution client",
		Image:      config.ExecutionImage,
		Entrypoint: []string{"/bin/sh", startScriptPath},
		Env:        executionEnv(peer.networkID, config.SyncMode),
		Files: []sidecar.File{
			{Name: startScriptPath, Body: []byte(startScript), Mode: 0o755},
			{Name: genesisPath, Body: files.genesis},
			{Name: jwtPath, Body: jwt},
		},
		Port:    rpcPort,
		Network: networkName,
		Aliases: []string{executionAlias},
	})
	if err != nil {
		return nil, err
	}
	if err := node.waitForExecution(ctx); err != nil {
		return nil, errors.Join(err, node.closeContainers())
	}

	node.consensus, err = sidecar.Start(ctx, client, sidecar.Spec{
		Name:  config.Name + " beacon node",
		Image: config.ConsensusImage,
		Cmd:   beaconCommand(executionAlias, peer, config.BeaconOrigin),
		Files: []sidecar.File{
			{Name: chainConfigPath, Body: files.chainConfig},
			{Name: genesisStatePath, Body: files.genesisState},
			{Name: jwtPath, Body: jwt},
		},
		Port:    beaconPort,
		Network: networkName,
	})
	if err != nil {
		return nil, errors.Join(err, node.closeContainers())
	}
	if err := node.waitForBeacon(ctx); err != nil {
		return nil, errors.Join(err, node.closeContainers())
	}
	return node, nil
}

func validate(config Config) error {
	if config.ExecutionImage == "" || config.ConsensusImage == "" {
		return errors.New("sync node needs an execution and a consensus image")
	}
	if config.SyncMode != "snap" && config.SyncMode != "full" {
		return fmt.Errorf("sync mode %q is not snap or full", config.SyncMode)
	}
	if config.BeaconOrigin != "" && config.BeaconOrigin != Genesis && config.BeaconOrigin != Checkpoint {
		return fmt.Errorf("beacon origin %q is not genesis or checkpoint", config.BeaconOrigin)
	}
	if config.Name == "" || strings.Trim(config.Name, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
		return fmt.Errorf("sync node name %q is not a DNS label", config.Name)
	}
	return nil
}

func executionEnv(networkID, syncMode string) []string {
	return []string{
		"DATA_DIR=" + executionDataDir,
		"GENESIS_FILE=" + genesisPath,
		"JWT_FILE=" + jwtPath,
		"NETWORK_ID=" + networkID,
		"SYNC_MODE=" + syncMode,
		"RPC_PORT=" + strconv.Itoa(rpcPort),
		"ENGINE_PORT=" + strconv.Itoa(enginePort),
	}
}

// beaconCommand runs the image's beacon-chain entrypoint. A beacon node with
// --min-sync-peers=0, as the devnet's own run, counts as synced before it has
// synced anything, so the node waits for its one peer. The genesis state is
// always loaded: a checkpoint-synced node still needs it to know the chain.
func beaconCommand(executionHost string, peer source, origin BeaconOrigin) []string {
	command := []string{
		"--accept-terms-of-use=true",
		"--datadir=" + beaconDataDir,
		"--execution-endpoint=http://" + net.JoinHostPort(executionHost, strconv.Itoa(enginePort)),
		"--jwt-secret=" + jwtPath,
		"--chain-config-file=" + chainConfigPath,
		"--genesis-state=" + genesisStatePath,
		"--contract-deployment-block=0",
		"--grpc-gateway-host=0.0.0.0",
		"--grpc-gateway-port=" + strconv.Itoa(beaconPort),
		"--no-discovery",
		"--peer=" + peer.beaconPeer,
		"--min-sync-peers=1",
		"--minimum-peers-per-subnet=0",
	}
	if origin == Checkpoint {
		command = append(command, "--checkpoint-sync-url="+peer.beaconAPI)
	}
	return command
}

type networkFiles struct {
	genesis      []byte
	chainConfig  []byte
	genesisState []byte
}

func readNetworkFiles(ctx context.Context, client devnet.ServiceFileClient, participant devnet.Participant) (networkFiles, error) {
	var files networkFiles
	for _, file := range []struct {
		serviceID string
		path      string
		body      *[]byte
	}{
		{participant.Execution.ID, genesisPath, &files.genesis},
		{participant.Consensus.ID, chainConfigPath, &files.chainConfig},
		{participant.Consensus.ID, genesisStatePath, &files.genesisState},
	} {
		body, err := devnet.ReadServiceFile(ctx, client, file.serviceID, file.path)
		if err != nil {
			return networkFiles{}, fmt.Errorf("copy %s from the source: %w", file.path, err)
		}
		if len(body) == 0 {
			return networkFiles{}, fmt.Errorf("source %s is empty", file.path)
		}
		*file.body = body
	}
	return files, nil
}

// newJWTSecret makes the secret the node's two clients share. It is not the
// devnet's, so the node's beacon node cannot drive a source client.
func newJWTSecret() ([]byte, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate JWT secret: %w", err)
	}
	return []byte(hexutil.Encode(secret)), nil
}

// querySource reads the source's network ID and the addresses its clients
// listen on, rewritten to their private IPs on the enclave network.
func querySource(ctx context.Context, participant devnet.Participant) (source, error) {
	execution, err := qrlclient.DialContext(ctx, participant.Execution.RPCURL)
	if err != nil {
		return source{}, fmt.Errorf("dial execution client: %w", err)
	}
	defer execution.Close()
	networkID, err := execution.NetworkID(ctx)
	if err != nil {
		return source{}, fmt.Errorf("read network ID: %w", err)
	}
	var info p2p.NodeInfo
	if err := execution.Client().CallContext(ctx, &info, "admin_nodeInfo"); err != nil {
		return source{}, fmt.Errorf("read execution node info: %w", err)
	}
	qnode, err := privateQnode(info.Qnode, participant.Execution.PrivateIP)
	if err != nil {
		return source{}, err
	}

	beaconClient, err := beacon.New(participant.Consensus.URL)
	if err != nil {
		return source{}, err
	}
	identity, err := beaconClient.Identity(ctx)
	if err != nil {
		return source{}, fmt.Errorf("read beacon identity: %w", err)
	}
	peer, err := privateBeaconPeer(identity, participant.Consensus.PrivateIP)
	if err != nil {
		return source{}, err
	}
	return source{
		networkID:  networkID.String(),
		qnode:      qnode,
		beaconPeer: peer,
		beaconAPI:  "http://" + net.JoinHostPort(participant.Consensus.PrivateIP, strconv.Itoa(beaconPort)),
	}, nil
}

// privateQnode points a qnode URL at privateIP, keeping its node ID and
// listening port and dropping the discovery port the node does not use.
func privateQnode(qnode, privateIP string) (string, error) {
	parsed, err := url.Parse(qnode)
	if err != nil || parsed.Scheme != "qnode" || parsed.User == nil || parsed.Port() == "" {
		return "", fmt.Errorf("source qnode %q is not a qnode://<id>@<ip>:<port> URL", qnode)
	}
	if net.ParseIP(privateIP) == nil {
		return "", fmt.Errorf("source execution private IP %q is invalid", privateIP)
	}
	parsed.Host = net.JoinHostPort(privateIP, parsed.Port())
	parsed.RawQuery = ""
	return parsed.String(), nil
}

// privateBeaconPeer builds the source beacon node's libp2p address at
// privateIP, on the TCP port it reports.
func privateBeaconPeer(identity beacon.Identity, privateIP string) (string, error) {
	if identity.PeerID == "" {
		return "", errors.New("source beacon node reports no peer ID")
	}
	if net.ParseIP(privateIP) == nil {
		return "", fmt.Errorf("source beacon private IP %q is invalid", privateIP)
	}
	for _, address := range identity.P2PAddresses {
		parts := strings.Split(address, "/")
		for index := 0; index+1 < len(parts); index++ {
			if parts[index] == "tcp" {
				return "/ip4/" + privateIP + "/tcp/" + parts[index+1] + "/p2p/" + identity.PeerID, nil
			}
		}
	}
	return "", fmt.Errorf("source beacon node reports no TCP address in %q", identity.P2PAddresses)
}

// waitForExecution waits until the execution client answers, then adds the
// source as its peer. The peer is not stored, so a restarted client needs it
// again.
func (node *Node) waitForExecution(ctx context.Context) error {
	return poll(ctx, node.execution, func(ctx context.Context, hostPort string) error {
		if node.Execution == nil {
			node.RPCURL = "http://" + hostPort
			client, err := qrlclient.DialContext(ctx, node.RPCURL)
			if err != nil {
				return err
			}
			node.Execution = client
		}
		var added bool
		if err := node.Execution.Client().CallContext(ctx, &added, "admin_addPeer", node.sourceQnode); err != nil {
			return fmt.Errorf("add the source peer: %w", err)
		}
		if !added {
			return errors.New("execution client did not add the source peer")
		}
		return nil
	})
}

func (node *Node) waitForBeacon(ctx context.Context) error {
	return poll(ctx, node.consensus, func(ctx context.Context, hostPort string) error {
		if node.Beacon == nil {
			node.BeaconURL = "http://" + hostPort
			client, err := beacon.New(node.BeaconURL)
			if err != nil {
				return err
			}
			node.Beacon = client
		}
		_, err := node.Beacon.SyncStatus(ctx)
		return err
	})
}

// poll calls ready with the container's published host:port until it
// succeeds, failing at once if the container stops.
func poll(ctx context.Context, container *sidecar.Container, ready func(context.Context, string) error) error {
	ticker := time.NewTicker(readyPollInterval)
	defer ticker.Stop()
	for {
		hostPort, err := container.PublishedPort(ctx)
		var exitErr *sidecar.ExitError
		if errors.As(err, &exitErr) {
			return err
		}
		if err == nil {
			if err = ready(ctx, net.JoinHostPort("127.0.0.1", hostPort)); err == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return container.WithLogs(fmt.Errorf("wait for the client to answer: %w", errors.Join(err, context.Cause(ctx))))
		case <-ticker.C:
		}
	}
}

// Stop shuts the beacon node down, then the execution client. Both keep
// their data for Resume.
func (node *Node) Stop(ctx context.Context) error {
	if err := node.consensus.Stop(ctx, beaconStopTimeout); err != nil {
		return err
	}
	return node.execution.Stop(ctx, executionStopTimeout)
}

// Resume starts a stopped node again from its data and waits until both
// clients answer, as Start does.
func (node *Node) Resume(ctx context.Context) error {
	if err := node.execution.Start(ctx); err != nil {
		return err
	}
	if err := node.waitForExecution(ctx); err != nil {
		return err
	}
	if err := node.consensus.Start(ctx); err != nil {
		return err
	}
	return node.waitForBeacon(ctx)
}

// ExecutionLog returns the execution client's whole output.
func (node *Node) ExecutionLog() (string, error) {
	return node.execution.Logs(0)
}

// Logs returns the end of both clients' output, and nothing for a node that
// never started.
func (node *Node) Logs() (string, error) {
	if node == nil {
		return "", nil
	}
	var sections []string
	var errs []error
	for _, container := range []*sidecar.Container{node.execution, node.consensus} {
		if container == nil {
			continue
		}
		logs, err := container.Logs(failureLogLines)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		sections = append(sections, logs)
	}
	return strings.Join(sections, "\n---\n"), errors.Join(errs...)
}

// Close removes both containers and closes the clients.
func (node *Node) Close() error {
	if node == nil {
		return nil
	}
	err := node.closeContainers()
	if node.closeClient != nil {
		err = errors.Join(err, node.closeClient())
	}
	return err
}

func (node *Node) closeContainers() error {
	if node.Execution != nil {
		node.Execution.Close()
	}
	return errors.Join(node.consensus.Close(), node.execution.Close())
}
