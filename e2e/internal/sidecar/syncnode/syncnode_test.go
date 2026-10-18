package syncnode

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/sidecartest"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func testSource(context.Context, devnet.Participant) (source, error) {
	return source{
		networkID:  "1337",
		qnode:      sourceQnode,
		beaconPeer: "/ip4/172.16.0.12/tcp/13000/p2p/16Uiu2",
		beaconAPI:  "http://172.16.0.12:3500",
	}, nil
}

// newDocker serves both of the node's containers from one fake whose
// published port is a server answering as both clients. addedPeers records
// the admin_addPeer calls.
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

func TestStartLaunchesBothClients(t *testing.T) {
	docker, addedPeers := newDocker(t)

	node, err := start(t.Context(), testConfig(), docker, testSource)
	require.NoError(t, err)
	require.Len(t, docker.Creates, 2)

	execution := docker.Creates[0]
	require.Equal(t, "gqrl:test", execution.Config.Image)
	require.Contains(t, execution.Config.Env, "NETWORK_ID=1337")
	require.Contains(t, execution.Config.Env, "SYNC_MODE=snap")
	require.Equal(t, containertypes.NetworkMode("kt-devnet"), execution.HostConfig.NetworkMode)
	require.Equal(t, []string{"qrl-tests-sync-node-execution"},
		execution.NetworkingConfig.EndpointsConfig["kt-devnet"].Aliases)

	consensus := docker.Creates[1]
	require.Equal(t, "beacon:test", consensus.Config.Image)
	require.Empty(t, consensus.Config.Entrypoint, "the beacon image's own entrypoint runs")
	require.Contains(t, consensus.Config.Cmd, "--execution-endpoint=http://qrl-tests-sync-node-execution:8551")
	require.Contains(t, consensus.Config.Cmd, "--peer=/ip4/172.16.0.12/tcp/13000/p2p/16Uiu2")
	require.Contains(t, consensus.Config.Cmd, "--min-sync-peers=1")
	require.Contains(t, consensus.Config.Cmd, "--genesis-state="+genesisStatePath)
	for _, flag := range consensus.Config.Cmd {
		require.NotContains(t, flag, "--checkpoint-sync-url", "a node syncing from genesis has no checkpoint")
	}
	require.Equal(t, containertypes.NetworkMode("kt-devnet"), consensus.HostConfig.NetworkMode)

	require.Equal(t, []string{sourceQnode}, *addedPeers)
	require.Equal(t, "http://127.0.0.1:"+docker.HostPort, node.RPCURL)
	require.Equal(t, "http://127.0.0.1:"+docker.HostPort, node.BeaconURL)

	require.NoError(t, node.Stop(t.Context()))
	require.Len(t, docker.Stopped, 2)
	require.NoError(t, node.Resume(t.Context()))
	require.Equal(t, []string{sourceQnode, sourceQnode}, *addedPeers, "a restarted client needs its peer again")

	require.NoError(t, node.Close())
	require.Len(t, docker.Removed, 2)
}

func TestStartFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		edit    func(*Config, *sidecartest.Docker)
		query   sourceQuery
		wantErr string
		created int
	}{
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
			query: func(context.Context, devnet.Participant) (source, error) {
				return source{}, errors.New("connection refused")
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
				query = testSource
			}

			_, err := start(t.Context(), config, docker, query)
			require.ErrorContains(t, err, test.wantErr)
			require.Len(t, docker.Creates, test.created)
			require.Len(t, docker.Removed, test.created, "every created container is removed")
		})
	}
}

func TestPrivateQnode(t *testing.T) {
	qnode, err := privateQnode("qnode://abcd@127.0.0.1:30303?discport=30301", "172.16.0.11")
	require.NoError(t, err)
	require.Equal(t, "qnode://abcd@172.16.0.11:30303", qnode)

	_, err = privateQnode("enode://abcd@127.0.0.1:30303", "172.16.0.11")
	require.ErrorContains(t, err, "is not a qnode://<id>@<ip>:<port> URL")
	_, err = privateQnode("qnode://abcd@127.0.0.1", "172.16.0.11")
	require.ErrorContains(t, err, "is not a qnode://<id>@<ip>:<port> URL")
	_, err = privateQnode("qnode://abcd@127.0.0.1:30303", "")
	require.ErrorContains(t, err, `source execution private IP "" is invalid`)
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

func TestJWTSecret(t *testing.T) {
	secret, err := newJWTSecret()
	require.NoError(t, err)
	require.Regexp(t, `^0x[0-9a-f]{64}$`, string(secret))
}

func TestStartFromCheckpoint(t *testing.T) {
	docker, _ := newDocker(t)
	config := testConfig()
	config.BeaconOrigin = Checkpoint

	node, err := start(t.Context(), config, docker, testSource)
	require.NoError(t, err)
	defer func() { require.NoError(t, node.Close()) }()
	require.Len(t, docker.Creates, 2)

	command := docker.Creates[1].Config.Cmd
	require.Contains(t, command, "--checkpoint-sync-url=http://172.16.0.12:3500")
	require.Contains(t, command, "--genesis-state="+genesisStatePath, "the genesis state is loaded along with the checkpoint")
}

func TestBeaconCommandReachesExecutionByAlias(t *testing.T) {
	peer, err := testSource(t.Context(), devnet.Participant{})
	require.NoError(t, err)
	for _, origin := range []BeaconOrigin{"", Genesis, Checkpoint} {
		command := beaconCommand("qrl-tests-a-execution", peer, origin)
		require.Contains(t, command, "--execution-endpoint=http://"+net.JoinHostPort("qrl-tests-a-execution", "8551"))
		require.Contains(t, command, "--peer=/ip4/172.16.0.12/tcp/13000/p2p/16Uiu2")
		require.Contains(t, command, "--no-discovery")
		require.Equal(t, origin == Checkpoint, slices.Contains(command, "--checkpoint-sync-url=http://172.16.0.12:3500"), "origin %q", origin)
	}
}
