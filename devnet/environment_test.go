package devnet

import (
	"testing"

	"github.com/cyyber/qrl-tests/devnet/internal/kurtosis"
	"github.com/stretchr/testify/require"
)

func TestParticipantsFromServices(t *testing.T) {
	services := map[string]kurtosis.Service{
		"cl-2-qrysm-gqrl": service("cl-2-qrysm-gqrl", "beacon", map[string]uint16{"http": 4202, "metrics": 4302}),
		"el-2-gqrl-qrysm": service("el-2-gqrl-qrysm", "execution", map[string]uint16{"rpc": 3202, "ws": 3302, "engine-rpc": 3402}),
		"vc-2-gqrl-qrysm": service("vc-2-gqrl-qrysm", "validator", map[string]uint16{"http-validator": 5202, "metrics": 5302}),
		"cl-1-qrysm-gqrl": service("cl-1-qrysm-gqrl", "beacon", map[string]uint16{"http": 4201, "metrics": 4301}),
		"el-1-gqrl-qrysm": service("el-1-gqrl-qrysm", "execution", map[string]uint16{"rpc": 3201, "ws": 3301, "engine-rpc": 3401}),
		"vc-1-gqrl-qrysm": service("vc-1-gqrl-qrysm", "validator", map[string]uint16{"http-validator": 5201, "metrics": 5301}),
		"prometheus":      {Labels: map[string]string{"qrl-package.client-type": "utility"}},
	}

	participants, err := endpointResolver{mode: EndpointModePublic}.participantsFromServices(services)
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
				URL:         "http://127.0.0.1:4201", MetricsURL: "http://127.0.0.1:4301/metrics",
			},
			Validator: ValidatorService{
				ServiceInfo: ServiceInfo{Name: "vc-1-gqrl-qrysm", ID: "vc-1-gqrl-qrysm-id", PrivateIP: "10.0.0.1"},
				URL:         "http://127.0.0.1:5201", MetricsURL: "http://127.0.0.1:5301/metrics",
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
				URL:         "http://127.0.0.1:4202", MetricsURL: "http://127.0.0.1:4302/metrics",
			},
			Validator: ValidatorService{
				ServiceInfo: ServiceInfo{Name: "vc-2-gqrl-qrysm", ID: "vc-2-gqrl-qrysm-id", PrivateIP: "10.0.0.2"},
				URL:         "http://127.0.0.1:5202", MetricsURL: "http://127.0.0.1:5302/metrics",
			},
		},
	}, participants)
}

func TestParticipantsFromUnlabeledKubernetesServices(t *testing.T) {
	execution := service("el-1-gqrl-qrysm", "execution", map[string]uint16{"rpc": 3201, "ws": 3301})
	execution.Labels = nil
	consensus := service("cl-1-qrysm-gqrl", "beacon", map[string]uint16{"http": 4201})
	consensus.Labels = nil

	participants, err := endpointResolver{mode: EndpointModePublic}.participantsFromServices(map[string]kurtosis.Service{
		"el-1-gqrl-qrysm": execution,
		"cl-1-qrysm-gqrl": consensus,
		"python-jobs":     {},
	})
	require.NoError(t, err)
	require.Len(t, participants, 1)
	require.Equal(t, "http://127.0.0.1:3201", participants[0].Execution.RPCURL)
	require.Equal(t, "http://127.0.0.1:4201", participants[0].Consensus.URL)
}

func TestClusterEndpoints(t *testing.T) {
	execution := service("el-1-gqrl-qrysm", "execution", map[string]uint16{"rpc": 3201, "ws": 3301, "metrics": 3501})
	execution.PrivatePorts = map[string]uint16{"rpc": 8545, "ws": 8546, "metrics": 9001}
	consensus := service("cl-1-qrysm-gqrl", "beacon", map[string]uint16{"http": 4201})
	consensus.PrivatePorts = map[string]uint16{"http": 3500, "metrics": 8080}
	services := map[string]kurtosis.Service{
		"el-1-gqrl-qrysm": execution,
		"cl-1-qrysm-gqrl": consensus,
	}

	participants, err := endpointResolver{mode: EndpointModeCluster}.participantsFromServices(services)
	require.NoError(t, err)
	require.Len(t, participants, 1)
	require.Equal(t, "http://10.0.0.1:8545", participants[0].Execution.RPCURL)
	require.Equal(t, "ws://10.0.0.1:8546", participants[0].Execution.WebSocketURL)
	require.Equal(t, "http://10.0.0.1:9001/debug/metrics/prometheus", participants[0].Execution.MetricsURL)
	require.Empty(t, participants[0].Execution.EngineURL)
	require.Equal(t, "http://10.0.0.1:3500", participants[0].Consensus.URL)
	require.Equal(t, "http://10.0.0.1:8080/metrics", participants[0].Consensus.MetricsURL)

	// Public mode of the same services resolves the published ports.
	participants, err = endpointResolver{mode: EndpointModePublic}.participantsFromServices(services)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:3201", participants[0].Execution.RPCURL)
	require.Equal(t, "http://127.0.0.1:3501/debug/metrics/prometheus", participants[0].Execution.MetricsURL)
	require.Empty(t, participants[0].Consensus.MetricsURL)
}

func TestEnvironmentNamespace(t *testing.T) {
	require.Equal(t, "kt-soak", Environment{EnclaveName: "soak", Backend: BackendKubernetes}.Namespace())
	require.Empty(t, Environment{EnclaveName: "soak", Backend: BackendDocker}.Namespace())
}

func TestParseEndpointMode(t *testing.T) {
	mode, err := ParseEndpointMode("")
	require.NoError(t, err)
	require.Equal(t, EndpointModePublic, mode)

	mode, err = ParseEndpointMode(" cluster ")
	require.NoError(t, err)
	require.Equal(t, EndpointModeCluster, mode)

	_, err = ParseEndpointMode("private")
	require.ErrorContains(t, err, "unsupported endpoint mode")
}

func TestParticipantIndex(t *testing.T) {
	index, err := participantIndex("service-without-an-index", map[string]string{"qrl-tests.participant": "7"})
	require.NoError(t, err)
	require.Equal(t, 7, index)

	index, err = participantIndex("el-2-gqrl-qrysm", nil)
	require.NoError(t, err)
	require.Equal(t, 2, index)

	for name, test := range map[string]struct {
		service string
		labels  map[string]string
		wantErr string
	}{
		"non-numeric label":  {service: "el-1-x", labels: map[string]string{"qrl-tests.participant": "seven"}, wantErr: "invalid participant label"},
		"label below one":    {service: "el-1-x", labels: map[string]string{"qrl-tests.participant": "0"}, wantErr: "invalid participant label"},
		"name without index": {service: "prometheus", wantErr: "no participant index"},
		"non-numeric index":  {service: "el-x-gqrl", wantErr: "invalid participant index"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := participantIndex(test.service, test.labels)
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

// service builds a fake resolved Kurtosis service; names are shaped like
// "el-1-…", whose digit doubles as the participant octet of the private IP.
func service(name, clientType string, ports map[string]uint16) kurtosis.Service {
	return kurtosis.Service{
		UUID:        name + "-id",
		PrivateIP:   "10.0.0." + name[3:4],
		PublicIP:    "127.0.0.1",
		PublicPorts: ports,
		Labels:      map[string]string{"qrl-package.client-type": clientType},
	}
}
