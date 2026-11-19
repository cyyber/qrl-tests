// Package syncnode runs a gqrl and a Qrysm beacon node, without validators,
// that sync from a devnet participant over the enclave's Docker network.
package syncnode

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/internal/dockerapi"
	"github.com/golang-jwt/jwt/v4"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/p2p"
	"github.com/theQRL/go-qrl/p2p/qnode"
	"github.com/theQRL/go-qrl/qrlclient"
	"github.com/theQRL/go-qrl/rpc"
)

const (
	executionRPCPort     = 8545
	executionWSPort      = 8546
	executionEnginePort  = 8551
	executionMetricsPort = 6060
	beaconPort           = 3500
	sourceBeaconPort     = 3500

	networkConfigDir  = "/network-configs"
	genesisPath       = networkConfigDir + "/genesis.json"
	chainConfigPath   = networkConfigDir + "/config.yaml"
	genesisStatePath  = networkConfigDir + "/genesis.ssz"
	jwtPath           = "/jwt/jwtsecret"
	executionDataDir  = "/data/gqrl/execution-data"
	beaconDataDir     = "/data/qrysm/beacon-data"
	startScriptPath   = "/start-gqrl.sh"
	readyPollInterval = time.Second

	executionStopTimeout = time.Minute
	beaconStopTimeout    = 30 * time.Second

	failureLogLines = 100

	defaultMaxPeers = 50
)

const startScript = `#!/bin/sh
set -eu
if [ ! -d "${DATA_DIR}/gqrl/chaindata" ]; then
  gqrl init --datadir="${DATA_DIR}" "${GENESIS_FILE}"
fi
exec gqrl \
  --datadir="${DATA_DIR}" \
  --networkid="${NETWORK_ID}" \
  --syncmode="${SYNC_MODE}" \
  --maxpeers="${MAX_PEERS}" \
  --cache=512 \
  --nodiscover \
  --http \
  --http.addr=0.0.0.0 \
  --http.port="${RPC_PORT}" \
  --http.vhosts='*' \
  --http.api=admin,debug,qrl,net \
  --ws \
  --ws.addr=0.0.0.0 \
  --ws.port="${WS_PORT}" \
  --ws.origins='*' \
  --ws.api=admin,debug,qrl,net \
  --metrics \
  --metrics.addr=0.0.0.0 \
  --metrics.port="${METRICS_PORT}" \
  --authrpc.addr=0.0.0.0 \
  --authrpc.port="${ENGINE_PORT}" \
  --authrpc.vhosts='*' \
  --authrpc.jwtsecret="${JWT_FILE}"
`

type Config struct {
	// Name tells nodes apart in errors and in the enclave's DNS.
	Name           string
	ExecutionImage string
	ConsensusImage string
	// SyncMode is gqrl's --syncmode, "snap" or "full".
	SyncMode      string
	BeaconOrigin  BeaconOrigin
	Source        devnet.Participant
	ExecutionOnly bool
	MaxPeers      int
}

// BeaconOrigin is where the sync node's beacon node starts from: genesis, or
// the source's finalized checkpoint.
type BeaconOrigin string

const (
	Genesis    BeaconOrigin = ""
	Checkpoint BeaconOrigin = "checkpoint"
)

// Node is a running node that the caller must Close.
type Node struct {
	Execution *qrlclient.Client
	Beacon    *beacon.Client
	Engine    *rpc.Client

	ExecutionWSURL      string
	ExecutionMetricsURL string

	executionAlias string

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
	node, err := start(ctx, config, client, querySourceEndpoints)
	if err != nil {
		return nil, errors.Join(err, client.Close())
	}
	node.closeClient = client.Close
	return node, nil
}

type sourceEndpoints struct {
	networkID  string
	qnode      string
	beaconPeer string
	beaconAPI  string
}

type sourceEndpointsQuery func(context.Context, devnet.Participant) (sourceEndpoints, error)

func start(ctx context.Context, config Config, client dockerClient, querySource sourceEndpointsQuery) (*Node, error) {
	if err := validate(config); err != nil {
		return nil, err
	}
	endpoints, err := querySource(ctx, config.Source)
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
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate JWT secret: %w", err)
	}
	jwtHex := []byte(hexutil.Encode(secret))

	executionAlias := "qrl-tests-" + config.Name + "-execution"
	node := &Node{sourceQnode: endpoints.qnode, executionAlias: executionAlias}
	node.execution, err = sidecar.Start(ctx, client, sidecar.Spec{
		Name:       config.Name + " execution client",
		Image:      config.ExecutionImage,
		Entrypoint: []string{"/bin/sh", startScriptPath},
		Env:        executionEnv(endpoints.networkID, config.SyncMode, cmp.Or(config.MaxPeers, defaultMaxPeers)),
		Files: []sidecar.File{
			{Name: startScriptPath, Body: []byte(startScript), Mode: 0o755},
			{Name: genesisPath, Body: files.genesis},
			{Name: jwtPath, Body: jwtHex},
		},
		Ports:   []uint16{executionRPCPort, executionWSPort, executionEnginePort, executionMetricsPort},
		Network: networkName,
		Aliases: []string{executionAlias},
	})
	if err != nil {
		return nil, err
	}
	if err := node.waitForExecution(ctx); err != nil {
		return nil, errors.Join(err, node.closeContainers())
	}
	if err := node.setExecutionEndpoints(ctx, secret); err != nil {
		return nil, errors.Join(err, node.closeContainers())
	}
	if config.ExecutionOnly {
		return node, nil
	}

	node.consensus, err = sidecar.Start(ctx, client, sidecar.Spec{
		Name:  config.Name + " beacon node",
		Image: config.ConsensusImage,
		Cmd:   beaconCommand(executionAlias, endpoints, config.BeaconOrigin),
		Files: []sidecar.File{
			{Name: chainConfigPath, Body: files.chainConfig},
			{Name: genesisStatePath, Body: files.genesisState},
			{Name: jwtPath, Body: jwtHex},
		},
		Ports:   []uint16{beaconPort},
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
	if config.ExecutionImage == "" {
		return errors.New("sync node needs an execution image")
	}
	if config.ConsensusImage == "" && !config.ExecutionOnly {
		return errors.New("sync node needs a consensus image")
	}
	if config.SyncMode != "snap" && config.SyncMode != "full" {
		return fmt.Errorf("sync mode %q is not snap or full", config.SyncMode)
	}
	if config.MaxPeers < 0 {
		return fmt.Errorf("max peers %d is negative", config.MaxPeers)
	}
	if config.BeaconOrigin != Genesis && config.BeaconOrigin != Checkpoint {
		return fmt.Errorf("beacon origin %q is not genesis or checkpoint", config.BeaconOrigin)
	}
	if config.Name == "" || strings.Trim(config.Name, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
		return fmt.Errorf("sync node name %q is not a DNS label", config.Name)
	}
	return nil
}

func executionEnv(networkID, syncMode string, maxPeers int) []string {
	return []string{
		"MAX_PEERS=" + strconv.Itoa(maxPeers),
		"DATA_DIR=" + executionDataDir,
		"GENESIS_FILE=" + genesisPath,
		"JWT_FILE=" + jwtPath,
		"NETWORK_ID=" + networkID,
		"SYNC_MODE=" + syncMode,
		"RPC_PORT=" + strconv.Itoa(executionRPCPort),
		"WS_PORT=" + strconv.Itoa(executionWSPort),
		"METRICS_PORT=" + strconv.Itoa(executionMetricsPort),
		"ENGINE_PORT=" + strconv.Itoa(executionEnginePort),
	}
}

func beaconCommand(executionHost string, endpoints sourceEndpoints, origin BeaconOrigin) []string {
	command := []string{
		"--accept-terms-of-use=true",
		"--datadir=" + beaconDataDir,
		"--execution-endpoint=http://" + net.JoinHostPort(executionHost, strconv.Itoa(executionEnginePort)),
		"--jwt-secret=" + jwtPath,
		"--chain-config-file=" + chainConfigPath,
		"--genesis-state=" + genesisStatePath,
		"--grpc-gateway-host=0.0.0.0",
		"--grpc-gateway-port=" + strconv.Itoa(beaconPort),
		"--no-discovery",
		"--peer=" + endpoints.beaconPeer,
		"--min-sync-peers=1",
		"--minimum-peers-per-subnet=0",
	}
	if origin == Checkpoint {
		command = append(command, "--checkpoint-sync-url="+endpoints.beaconAPI)
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

// querySourceEndpoints reads the source's network ID and the addresses its clients
// listen on, rewritten to their private IPs on the enclave network.
func querySourceEndpoints(ctx context.Context, participant devnet.Participant) (sourceEndpoints, error) {
	execution, err := qrlclient.DialContext(ctx, participant.Execution.RPCURL)
	if err != nil {
		return sourceEndpoints{}, fmt.Errorf("dial execution client: %w", err)
	}
	defer execution.Close()
	networkID, err := execution.NetworkID(ctx)
	if err != nil {
		return sourceEndpoints{}, fmt.Errorf("read network ID: %w", err)
	}
	var info p2p.NodeInfo
	if err := execution.Client().CallContext(ctx, &info, "admin_nodeInfo"); err != nil {
		return sourceEndpoints{}, fmt.Errorf("read execution node info: %w", err)
	}
	qnode, err := privateQnode(info.Qnode, participant.Execution.PrivateIP)
	if err != nil {
		return sourceEndpoints{}, err
	}

	beaconClient, err := beacon.New(participant.Consensus.URL)
	if err != nil {
		return sourceEndpoints{}, err
	}
	identity, err := beaconClient.Identity(ctx)
	if err != nil {
		return sourceEndpoints{}, fmt.Errorf("read beacon identity: %w", err)
	}
	peer, err := privateBeaconPeer(identity, participant.Consensus.PrivateIP)
	if err != nil {
		return sourceEndpoints{}, err
	}
	return sourceEndpoints{
		networkID:  networkID.String(),
		qnode:      qnode,
		beaconPeer: peer,
		beaconAPI:  "http://" + net.JoinHostPort(participant.Consensus.PrivateIP, strconv.Itoa(sourceBeaconPort)),
	}, nil
}

// privateQnode points a qnode URL at privateIP.
func privateQnode(rawURL, privateIP string) (string, error) {
	if net.ParseIP(privateIP) == nil {
		return "", fmt.Errorf("source execution private IP %q is invalid", privateIP)
	}
	return qnodeAt(rawURL, privateIP)
}

// qnodeAt points a qnode URL at host, keeping its node ID and listening port
// and dropping the discovery port the node does not use.
func qnodeAt(rawURL, host string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "qnode" || parsed.User == nil || parsed.Port() == "" {
		return "", fmt.Errorf("qnode %q is not a qnode://<id>@<ip>:<port> URL", rawURL)
	}
	parsed.Host = net.JoinHostPort(host, parsed.Port())
	parsed.RawQuery = ""
	return parsed.String(), nil
}

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

// setExecutionEndpoints sets the execution client's endpoints other than
// its RPC API. Their host ports stay the same when the client is restarted.
func (node *Node) setExecutionEndpoints(ctx context.Context, jwtSecret []byte) error {
	ws, err := node.execution.PublishedAddress(ctx, executionWSPort)
	if err != nil {
		return err
	}
	metrics, err := node.execution.PublishedAddress(ctx, executionMetricsPort)
	if err != nil {
		return err
	}
	engine, err := node.execution.PublishedAddress(ctx, executionEnginePort)
	if err != nil {
		return err
	}

	node.Engine, err = rpc.DialOptions(ctx, "http://"+engine, rpc.WithHTTPAuth(engineAuth(jwtSecret)))
	if err != nil {
		return fmt.Errorf("dial the engine API: %w", err)
	}
	node.ExecutionWSURL = "ws://" + ws
	node.ExecutionMetricsURL = "http://" + metrics + "/debug/metrics/prometheus"
	return nil
}

// engineAuth signs a fresh token for each engine API request; the execution
// client accepts a token for a minute after it was issued.
func engineAuth(secret []byte) rpc.HTTPAuth {
	return func(header http.Header) error {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"iat": jwt.NewNumericDate(time.Now())})
		signed, err := token.SignedString(secret)
		if err != nil {
			return fmt.Errorf("sign engine API token: %w", err)
		}
		header.Set("Authorization", "Bearer "+signed)
		return nil
	}
}

func (node *Node) waitForExecution(ctx context.Context) error {
	return poll(ctx, node.execution, executionRPCPort, func(ctx context.Context, address string) error {
		if node.Execution == nil {
			client, err := qrlclient.DialContext(ctx, "http://"+address)
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
	return poll(ctx, node.consensus, beaconPort, func(ctx context.Context, address string) error {
		if node.Beacon == nil {
			client, err := beacon.New("http://" + address)
			if err != nil {
				return err
			}
			node.Beacon = client
		}
		_, err := node.Beacon.SyncStatus(ctx)
		return err
	})
}

// poll calls ready with the published address of the container's port until
// ready succeeds. It fails at once if the container stops.
func poll(ctx context.Context, container *sidecar.Container, port uint16, ready func(context.Context, string) error) error {
	ticker := time.NewTicker(readyPollInterval)
	defer ticker.Stop()
	for {
		address, err := container.PublishedAddress(ctx, port)
		var exitErr *sidecar.ExitError
		if errors.As(err, &exitErr) {
			return err
		}
		if err == nil {
			if err = ready(ctx, address); err == nil {
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

func (node *Node) Stop(ctx context.Context) error {
	if node.consensus != nil {
		if err := node.consensus.Stop(ctx, beaconStopTimeout); err != nil {
			return err
		}
	}
	return node.execution.Stop(ctx, executionStopTimeout)
}

func (node *Node) Resume(ctx context.Context) error {
	if err := node.execution.Start(ctx); err != nil {
		return err
	}
	if err := node.waitForExecution(ctx); err != nil {
		return err
	}
	if node.consensus == nil {
		return nil
	}
	if err := node.consensus.Start(ctx); err != nil {
		return err
	}
	return node.waitForBeacon(ctx)
}

// Peer returns the execution client's qnode URL, addressed by its alias on the
// enclave network, and its node ID.
func (node *Node) Peer(ctx context.Context) (string, qnode.ID, error) {
	var info p2p.NodeInfo
	if err := node.Execution.Client().CallContext(ctx, &info, "admin_nodeInfo"); err != nil {
		return "", qnode.ID{}, fmt.Errorf("read execution node info: %w", err)
	}
	address, err := qnodeAt(info.Qnode, node.executionAlias)
	if err != nil {
		return "", qnode.ID{}, err
	}
	id, err := qnode.ParseID(info.ID)
	if err != nil {
		return "", qnode.ID{}, fmt.Errorf("execution node ID: %w", err)
	}
	return address, id, nil
}

// AddPeer makes peerURL a static peer of the execution client, which dials it
// again after a disconnect but forgets it when the client restarts.
func (node *Node) AddPeer(ctx context.Context, peerURL string) error {
	var added bool
	if err := node.Execution.Client().CallContext(ctx, &added, "admin_addPeer", peerURL); err != nil {
		return fmt.Errorf("add peer %s: %w", peerURL, err)
	}
	if !added {
		return fmt.Errorf("execution client did not add peer %s", peerURL)
	}
	return nil
}

// Peers returns the IDs of the execution client's peers.
func (node *Node) Peers(ctx context.Context) ([]qnode.ID, error) {
	var peers []p2p.PeerInfo
	if err := node.Execution.Client().CallContext(ctx, &peers, "admin_peers"); err != nil {
		return nil, fmt.Errorf("read execution peers: %w", err)
	}
	ids := make([]qnode.ID, len(peers))
	for index, peer := range peers {
		id, err := qnode.ParseID(peer.ID)
		if err != nil {
			return nil, fmt.Errorf("peer ID: %w", err)
		}
		ids[index] = id
	}
	return ids, nil
}

// ExecutionLog returns the execution client's whole output.
func (node *Node) ExecutionLog() (string, error) {
	return node.execution.Logs(0)
}

// BeaconLog returns the beacon node's whole output.
func (node *Node) BeaconLog() (string, error) {
	if node.consensus == nil {
		return "", nil
	}
	return node.consensus.Logs(0)
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
	if node.Engine != nil {
		node.Engine.Close()
	}
	return errors.Join(node.consensus.Close(), node.execution.Close())
}

// Containers returns the Docker IDs of the node's containers.
func (node *Node) Containers() []string {
	ids := []string{node.execution.ID()}
	if node.consensus != nil {
		ids = append(ids, node.consensus.ID())
	}
	return ids
}
