package devnet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyyber/qrl-tests/devnet/internal/kurtosis"
	"github.com/cyyber/qrl-tests/internal/devwallet"
	"github.com/stretchr/testify/require"
)

type startClient struct {
	created   bool
	destroyed bool
}

func (*startClient) EnclaveExists(context.Context, string) (bool, error) { return false, nil }

func (client *startClient) CreateAndRunRemotePackage(context.Context, string, string, string) (bool, error) {
	return client.created, errors.New("package failed")
}

func (*startClient) Services(context.Context, string) (map[string]kurtosis.Service, error) {
	return nil, nil
}

func (*startClient) StartServices(context.Context, string, ...string) error { return nil }

func (*startClient) StopServices(context.Context, string, ...string) error { return nil }

func (client *startClient) DestroyEnclave(context.Context, string) error {
	client.destroyed = true
	return nil
}

func TestStartCleansCreatedEnclave(t *testing.T) {
	client := &startClient{created: true}
	manager := &Manager{
		newClient: func() (kurtosisClient, error) { return client, nil },
		probe:     func(context.Context, string, string) error { return nil },
	}

	_, err := manager.Start(t.Context(), StartOptions{
		EnclaveName: "failed-start",
		Images:      Images{Execution: "go-qrl:test"},
		Profile:     ProfileSingle,
	})
	require.ErrorContains(t, err, "package failed")
	require.True(t, client.destroyed)
}

func TestParticipantsFromServices(t *testing.T) {
	services := map[string]kurtosis.Service{
		"cl-2-qrysm-gqrl": service("cl-2-qrysm-gqrl", "beacon", 4202, 0, 0, 0, 4302),
		"el-2-gqrl-qrysm": service("el-2-gqrl-qrysm", "execution", 3202, 3302, 3402, 0),
		"vc-2-gqrl-qrysm": service("vc-2-gqrl-qrysm", "validator", 0, 0, 0, 5202, 5302),
		"cl-1-qrysm-gqrl": service("cl-1-qrysm-gqrl", "beacon", 4201, 0, 0, 0, 4301),
		"el-1-gqrl-qrysm": service("el-1-gqrl-qrysm", "execution", 3201, 3301, 3401, 0),
		"vc-1-gqrl-qrysm": service("vc-1-gqrl-qrysm", "validator", 0, 0, 0, 5201, 5301),
		"prometheus":      {Labels: map[string]string{"qrl-package.client-type": "utility"}},
	}

	participants, err := participantsFromServices(services)
	require.NoError(t, err)
	require.Equal(t, []Participant{
		{
			Index: 1,
			Execution: ExecutionService{
				ServiceInfo: ServiceInfo{Name: "el-1-gqrl-qrysm", ID: "el-1-gqrl-qrysm-id", PrivateIP: "10.0.0.1"},
				RPCURL:      "http://127.0.0.1:3201", GraphQLURL: "http://127.0.0.1:3201/graphql",
				WebSocketURL: "ws://127.0.0.1:3301", EngineURL: "http://127.0.0.1:3401",
			},
			Consensus: ConsensusService{
				ServiceInfo: ServiceInfo{Name: "cl-1-qrysm-gqrl", ID: "cl-1-qrysm-gqrl-id", PrivateIP: "10.0.0.1"},
				URL:         "http://127.0.0.1:4201", MetricsURL: "http://127.0.0.1:4301",
			},
			Validator: ValidatorService{
				ServiceInfo: ServiceInfo{Name: "vc-1-gqrl-qrysm", ID: "vc-1-gqrl-qrysm-id", PrivateIP: "10.0.0.1"},
				URL:         "http://127.0.0.1:5201", MetricsURL: "http://127.0.0.1:5301",
			},
		},
		{
			Index: 2,
			Execution: ExecutionService{
				ServiceInfo: ServiceInfo{Name: "el-2-gqrl-qrysm", ID: "el-2-gqrl-qrysm-id", PrivateIP: "10.0.0.2"},
				RPCURL:      "http://127.0.0.1:3202", GraphQLURL: "http://127.0.0.1:3202/graphql",
				WebSocketURL: "ws://127.0.0.1:3302", EngineURL: "http://127.0.0.1:3402",
			},
			Consensus: ConsensusService{
				ServiceInfo: ServiceInfo{Name: "cl-2-qrysm-gqrl", ID: "cl-2-qrysm-gqrl-id", PrivateIP: "10.0.0.2"},
				URL:         "http://127.0.0.1:4202", MetricsURL: "http://127.0.0.1:4302",
			},
			Validator: ValidatorService{
				ServiceInfo: ServiceInfo{Name: "vc-2-gqrl-qrysm", ID: "vc-2-gqrl-qrysm-id", PrivateIP: "10.0.0.2"},
				URL:         "http://127.0.0.1:5202", MetricsURL: "http://127.0.0.1:5302",
			},
		},
	}, participants)
}

func TestParticipantIndexUsesLabel(t *testing.T) {
	index, err := participantIndex("service-without-an-index", map[string]string{"qrl-tests.participant": "7"})
	require.NoError(t, err)
	require.Equal(t, 7, index)

	index, err = participantIndex("el-2-gqrl-qrysm", nil)
	require.NoError(t, err)
	require.Equal(t, 2, index)
}

func service(name, clientType string, rpc, ws, engine, validator uint16, metrics ...uint16) kurtosis.Service {
	ports := map[string]uint16{}
	metricPort := uint16(0)
	if len(metrics) > 0 {
		metricPort = metrics[0]
	}
	for id, port := range map[string]uint16{
		"rpc": rpc, "ws": ws, "engine-rpc": engine, "http": rpc, "http-validator": validator, "metrics": metricPort,
	} {
		if port != 0 {
			ports[id] = port
		}
	}
	return kurtosis.Service{
		UUID: name + "-id", PrivateIP: "10.0.0." + name[3:4], PublicIP: "127.0.0.1", PublicPorts: ports,
		Labels: map[string]string{"qrl-package.client-type": clientType},
	}
}

func TestParseBackend(t *testing.T) {
	backend, err := ParseBackend("")
	require.NoError(t, err)
	require.Equal(t, BackendDocker, backend)

	backend, err = ParseBackend("kubernetes")
	require.NoError(t, err)
	require.Equal(t, BackendKubernetes, backend)

	_, err = ParseBackend("unknown")
	require.Error(t, err)
}

func TestKubernetesImagesUseRegistry(t *testing.T) {
	images := Images{
		Execution: "registry.example/go-qrl:test",
		Clef:      "registry.example/go-qrl-clef:test",
		Consensus: "registry.example/qrysm-beacon:test",
		Validator: "registry.example/qrysm-validator:test",
		Genesis:   "registry.example/qrl-genesis:test",
	}
	require.NoError(t, images.validate(BackendKubernetes))

	images.Execution = DefaultExecutionImage
	require.ErrorContains(t, images.validate(BackendKubernetes), "not available to Kubernetes")
	require.NoError(t, images.validate(BackendDocker))
}

func TestProbeNetwork(t *testing.T) {
	blockCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Method string   `json:"method"`
			Params []string `json:"params"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		switch payload.Method {
		case "qrl_blockNumber":
			blockCalls++
			fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":1,"result":"0x%x"}`, blockCalls)
		case "qrl_getBalance":
			require.Equal(t, []string{devwallet.Address, "latest"}, payload.Params)
			fmt.Fprint(writer, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
		default:
			t.Fatalf("unexpected RPC method %q", payload.Method)
		}
	}))
	defer server.Close()

	require.NoError(t, probeNetwork(context.Background(), server.URL, devwallet.Address))
	require.GreaterOrEqual(t, blockCalls, 2)
}
