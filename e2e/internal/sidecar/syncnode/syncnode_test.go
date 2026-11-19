package syncnode

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/sidecartest"
	"github.com/golang-jwt/jwt/v4"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/p2p/qnode"
	"github.com/theQRL/go-qrl/qrlclient"
)

var _ dockerClient = (*sidecartest.Docker)(nil)

const sourceQnode = "qnode://abcd@172.16.0.11:30303"

func testConfig() Config {
	return Config{
		Name:           "sync-node",
		ExecutionImage: "gqrl:test",
		ConsensusImage: "beacon:test",
		SyncMode:       "snap",
		Source: devnet.Participant{
			Index: 1,
			Execution: devnet.ExecutionService{ServiceInfo: devnet.ServiceInfo{
				Name: "el-1-gqrl-qrysm", ID: "execution-service", PrivateIP: "172.16.0.11",
			}},
			Consensus: devnet.ConsensusService{ServiceInfo: devnet.ServiceInfo{
				Name: "cl-1-qrysm-gqrl", ID: "consensus-service", PrivateIP: "172.16.0.12",
			}},
		},
	}
}

func testSourceEndpoints(context.Context, devnet.Participant) (sourceEndpoints, error) {
	return sourceEndpoints{
		networkID:  "1337",
		qnode:      sourceQnode,
		beaconPeer: "/ip4/172.16.0.12/tcp/13000/p2p/16Uiu2",
		beaconAPI:  "http://172.16.0.12:3500",
	}, nil
}

// newDocker is a fake Docker whose containers all publish one test server
// that answers as both the execution and the beacon client. addedPeers
// collects the peers passed to admin_addPeer.
func newDocker(t *testing.T) (docker *sidecartest.Docker, addedPeers *[]string) {
	t.Helper()
	peers := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/qrl/v1/node/syncing" {
			_, _ = writer.Write([]byte(`{"data":{"head_slot":"0","sync_distance":"0","is_syncing":true,"is_optimistic":false,"el_offline":false}}`))
			return
		}
		var call struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params []string        `json:"params"`
		}
		assert.NoError(t, json.NewDecoder(request.Body).Decode(&call))
		assert.Equal(t, "admin_addPeer", call.Method)
		peers = append(peers, call.Params...)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":` + string(call.ID) + `,"result":true}`))
	}))
	t.Cleanup(server.Close)
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	docker = sidecartest.NewDocker()
	docker.HostPort = serverURL.Port()
	docker.Containers = []containertypes.Summary{{
		ID: "source-container",
		NetworkSettings: &containertypes.NetworkSettingsSummary{Networks: map[string]*network.EndpointSettings{
			"kt-devnet": {IPAddress: netip.MustParseAddr("172.16.0.12")},
		}},
	}}
	docker.Files[genesisPath] = []byte(`{"config":{}}`)
	docker.Files[chainConfigPath] = []byte("PRESET_BASE: minimal\n")
	docker.Files[genesisStatePath] = []byte{0x01}
	return docker, &peers
}

func TestStartScriptVariablesAreSet(t *testing.T) {
	set := map[string]bool{}
	for _, variable := range executionEnv("1337", "snap") {
		name, _, _ := strings.Cut(variable, "=")
		set[name] = true
	}
	for _, match := range regexp.MustCompile(`\$\{([A-Z_]+)\}`).FindAllStringSubmatch(startScript, -1) {
		require.True(t, set[match[1]], "start script reads %s, which executionEnv does not set", match[1])
	}
}

// boundHostPort returns the host port a container binds its port to.
func boundHostPort(t *testing.T, host *containertypes.HostConfig, number uint16) string {
	t.Helper()
	port, ok := network.PortFrom(number, network.TCP)
	require.True(t, ok)
	bindings := host.PortBindings[port]
	require.Len(t, bindings, 1)
	return bindings[0].HostPort
}

func TestStartLaunchesBothClients(t *testing.T) {
	docker, addedPeers := newDocker(t)

	node, err := start(t.Context(), testConfig(), docker, testSourceEndpoints)
	require.NoError(t, err)
	require.Len(t, docker.Created, 2)

	execution := docker.Created[0]
	require.Equal(t, "gqrl:test", execution.Config.Image)
	require.Contains(t, execution.Config.Env, "NETWORK_ID=1337")
	require.Contains(t, execution.Config.Env, "SYNC_MODE=snap")
	require.Equal(t, containertypes.NetworkMode("kt-devnet"), execution.HostConfig.NetworkMode)
	require.Equal(t, []string{"qrl-tests-sync-node-execution"},
		execution.NetworkingConfig.EndpointsConfig["kt-devnet"].Aliases)
	require.Len(t, execution.Config.ExposedPorts, 4, "the RPC, WebSocket, engine and metrics ports")

	consensus := docker.Created[1]
	require.Equal(t, "beacon:test", consensus.Config.Image)
	require.Empty(t, consensus.Config.Entrypoint, "the beacon image's own entrypoint runs")
	require.Contains(t, consensus.Config.Cmd, "--execution-endpoint=http://qrl-tests-sync-node-execution:8551")
	require.Contains(t, consensus.Config.Cmd, "--peer=/ip4/172.16.0.12/tcp/13000/p2p/16Uiu2")
	require.Contains(t, consensus.Config.Cmd, "--min-sync-peers=1")
	require.Equal(t, containertypes.NetworkMode("kt-devnet"), consensus.HostConfig.NetworkMode)

	require.Equal(t, []string{sourceQnode}, *addedPeers)
	require.NotNil(t, node.Execution)
	require.NotNil(t, node.Beacon)
	require.NotNil(t, node.Engine)

	// Clearing HostPort makes the fake Docker report each port's own binding.
	shared := docker.HostPort
	docker.HostPort = ""
	node.Engine.Close()
	require.NoError(t, node.setExecutionEndpoints(t.Context(), nil))
	require.Equal(t, "ws://127.0.0.1:"+boundHostPort(t, execution.HostConfig, executionWSPort), node.ExecutionWSURL)
	require.Equal(t, "http://127.0.0.1:"+boundHostPort(t, execution.HostConfig, executionMetricsPort)+"/debug/metrics/prometheus",
		node.ExecutionMetricsURL)
	docker.HostPort = shared

	require.NoError(t, node.Stop(t.Context()))
	require.Len(t, docker.Stopped, 2)
	require.NoError(t, node.Resume(t.Context()))
	require.Equal(t, []string{sourceQnode, sourceQnode}, *addedPeers, "a restarted client needs its peer again")

	require.NoError(t, node.Close())
	require.Len(t, docker.Removed, 2)
}

func TestStartExecutionOnly(t *testing.T) {
	docker, addedPeers := newDocker(t)
	config := testConfig()
	config.ExecutionOnly = true
	config.ConsensusImage = ""

	node, err := start(t.Context(), config, docker, testSourceEndpoints)
	require.NoError(t, err)
	require.Len(t, docker.Created, 1, "only the execution client")
	require.NotNil(t, node.Execution)
	require.NotNil(t, node.Engine)
	require.Nil(t, node.Beacon)

	require.NoError(t, node.Stop(t.Context()))
	require.Len(t, docker.Stopped, 1)
	require.NoError(t, node.Resume(t.Context()))
	require.Equal(t, []string{sourceQnode, sourceQnode}, *addedPeers)
	log, err := node.BeaconLog()
	require.NoError(t, err)
	require.Empty(t, log)

	require.NoError(t, node.Close())
	require.Len(t, docker.Removed, 1)
}

func TestStartFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		edit    func(*Config, *sidecartest.Docker)
		query   sourceEndpointsQuery
		wantErr string
		created int
	}{
		{
			name:    "no execution image",
			edit:    func(config *Config, _ *sidecartest.Docker) { config.ExecutionImage = "" },
			wantErr: "sync node needs an execution image",
		},
		{
			name:    "no consensus image",
			edit:    func(config *Config, _ *sidecartest.Docker) { config.ConsensusImage = "" },
			wantErr: "sync node needs a consensus image",
		},
		{
			name:    "bad sync mode",
			edit:    func(config *Config, _ *sidecartest.Docker) { config.SyncMode = "light" },
			wantErr: `sync mode "light" is not snap or full`,
		},
		{
			name:    "bad beacon origin",
			edit:    func(config *Config, _ *sidecartest.Docker) { config.BeaconOrigin = "backfill" },
			wantErr: `beacon origin "backfill" is not genesis or checkpoint`,
		},
		{
			name:    "name is not a DNS label",
			edit:    func(config *Config, _ *sidecartest.Docker) { config.Name = "Sync Node" },
			wantErr: `sync node name "Sync Node" is not a DNS label`,
		},
		{
			name: "source does not answer",
			query: func(context.Context, devnet.Participant) (sourceEndpoints, error) {
				return sourceEndpoints{}, errors.New("connection refused")
			},
			wantErr: "query source participant 1: connection refused",
		},
		{
			name:    "no genesis state",
			edit:    func(_ *Config, docker *sidecartest.Docker) { delete(docker.Files, genesisStatePath) },
			wantErr: "copy /network-configs/genesis.ssz from the source",
		},
		{
			name: "execution client exits",
			edit: func(_ *Config, docker *sidecartest.Docker) {
				docker.State = &containertypes.State{Status: containertypes.StateExited, ExitCode: 1}
				docker.Logs = "Fatal: invalid genesis"
			},
			wantErr: "sync-node execution client container exited with code 1\nlast log lines:\nFatal: invalid genesis",
			created: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			docker, _ := newDocker(t)
			config := testConfig()
			if test.edit != nil {
				test.edit(&config, docker)
			}
			query := test.query
			if query == nil {
				query = testSourceEndpoints
			}

			_, err := start(t.Context(), config, docker, query)
			require.ErrorContains(t, err, test.wantErr)
			require.Len(t, docker.Created, test.created)
			require.Len(t, docker.Removed, test.created, "every created container is removed")
		})
	}
}

func TestPrivateQnode(t *testing.T) {
	private, err := privateQnode("qnode://abcd@127.0.0.1:30303?discport=30301", "172.16.0.11")
	require.NoError(t, err)
	require.Equal(t, "qnode://abcd@172.16.0.11:30303", private)

	_, err = privateQnode("enode://abcd@127.0.0.1:30303", "172.16.0.11")
	require.ErrorContains(t, err, "is not a qnode://<id>@<ip>:<port> URL")
	_, err = privateQnode("qnode://abcd@127.0.0.1", "172.16.0.11")
	require.ErrorContains(t, err, "is not a qnode://<id>@<ip>:<port> URL")
	_, err = privateQnode("qnode://abcd@127.0.0.1:30303", "")
	require.ErrorContains(t, err, `source execution private IP "" is invalid`)
}

func TestQnodeAt(t *testing.T) {
	aliased, err := qnodeAt("qnode://abcd@127.0.0.1:30303?discport=30301", "qrl-tests-capped-execution")
	require.NoError(t, err)
	require.Equal(t, "qnode://abcd@qrl-tests-capped-execution:30303", aliased)
}

func TestPrivateBeaconPeer(t *testing.T) {
	identity := beacon.Identity{
		PeerID:       "16Uiu2",
		P2PAddresses: []string{"/ip4/127.0.0.1/udp/12000/quic-v1", "/ip4/127.0.0.1/tcp/13000/p2p/16Uiu2"},
	}
	peer, err := privateBeaconPeer(identity, "172.16.0.12")
	require.NoError(t, err)
	require.Equal(t, "/ip4/172.16.0.12/tcp/13000/p2p/16Uiu2", peer)

	_, err = privateBeaconPeer(beacon.Identity{PeerID: "16Uiu2"}, "172.16.0.12")
	require.ErrorContains(t, err, "reports no TCP address")
	_, err = privateBeaconPeer(beacon.Identity{P2PAddresses: identity.P2PAddresses}, "172.16.0.12")
	require.EqualError(t, err, "source beacon node reports no peer ID")
	_, err = privateBeaconPeer(identity, "not-an-ip")
	require.ErrorContains(t, err, `source beacon private IP "not-an-ip" is invalid`)
}

func TestEngineAuth(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	header := http.Header{}
	require.NoError(t, engineAuth(secret)(header))

	signed, found := strings.CutPrefix(header.Get("Authorization"), "Bearer ")
	require.True(t, found)
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(signed, &claims, func(*jwt.Token) (any, error) { return secret, nil },
		jwt.WithValidMethods([]string{"HS256"}))
	require.NoError(t, err)
	require.WithinDuration(t, time.Now(), claims.IssuedAt.Time, time.Minute)

	_, err = jwt.Parse(signed, func(*jwt.Token) (any, error) { return []byte("another secret"), nil })
	require.ErrorIs(t, err, jwt.ErrSignatureInvalid)
}

func TestPeer(t *testing.T) {
	const id = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			ID json.RawMessage `json:"id"`
		}
		assert.NoError(t, json.NewDecoder(request.Body).Decode(&call))
		nodeInfo := `{"id":"` + id + `","qnode":"qnode://abcd@127.0.0.1:30303?discport=30301"}`
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":` + string(call.ID) + `,"result":` + nodeInfo + `}`))
	}))
	defer server.Close()
	client, err := qrlclient.DialContext(t.Context(), server.URL)
	require.NoError(t, err)
	node := &Node{Execution: client, executionAlias: "qrl-tests-capped-execution"}
	defer client.Close()

	address, nodeID, err := node.Peer(t.Context())
	require.NoError(t, err)
	require.Equal(t, "qnode://abcd@qrl-tests-capped-execution:30303", address)
	require.Equal(t, qnode.HexID(id), nodeID)
}
