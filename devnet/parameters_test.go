package devnet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cyyber/qrl-tests/internal/devwallet"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestDefaultParameters(t *testing.T) {
	address := "Q" + strings.Repeat("a", 128)
	executionImage := "ghcr.io/example/go-qrl@sha256:" + strings.Repeat("0af1", 16)
	payload, err := resolveParameters(address, StartOptions{
		Images:  Images{Execution: executionImage},
		Profile: ProfileSingle,
	})
	require.NoError(t, err)

	var parameters map[string]any
	require.NoError(t, json.Unmarshal([]byte(payload), &parameters))

	participant := parameters["participants"].([]any)[0].(map[string]any)
	network := parameters["network_params"].(map[string]any)
	prefund := network["prefunded_accounts"].(map[string]any)[address].(map[string]any)
	require.Equal(t, executionImage, participant["el_image"])
	require.Equal(t, DefaultConsensusImage, participant["cl_image"])
	require.Equal(t, DefaultValidatorImage, participant["vc_image"])
	require.Equal(t, true, participant["use_remote_signer"])
	require.Equal(t, "clef", participant["remote_signer_type"])
	require.Equal(t, DefaultClefImage, participant["remote_signer_image"])
	require.Equal(t, true, participant["remote_signer_auto_approve"])
	require.Equal(t, float64(64), participant["validator_count"])
	require.Equal(t, []any{"--graphql", "--graphql.vhosts=*"}, participant["el_extra_params"])
	require.Equal(t, []any{"--min-sync-peers=0", "--minimum-peers-per-subnet=0"}, participant["cl_extra_params"])
	require.Equal(t, []any{}, participant["vc_extra_params"])
	require.Equal(t, DefaultGenesisImage, parameters["qrl_genesis_generator_params"].(map[string]any)["image"])
	require.Equal(t, "1337", network["network_id"])
	require.Equal(t, address, network["withdrawal_address"])
	require.Equal(t, "2000000QRL", prefund["balance"])
}

func soakImages() Images {
	return Images{
		Execution:       "ghcr.io/example/go-qrl@sha256:" + strings.Repeat("0af1", 16),
		Clef:            "ghcr.io/example/go-qrl-clef@sha256:" + strings.Repeat("0af2", 16),
		Consensus:       "ghcr.io/example/qrysm-beacon@sha256:" + strings.Repeat("0af3", 16),
		Validator:       "ghcr.io/example/qrysm-validator@sha256:" + strings.Repeat("0af4", 16),
		Genesis:         "ghcr.io/example/qrl-genesis-generator@sha256:" + strings.Repeat("0af5", 16),
		TxSpammer:       "ghcr.io/example/qrl-tx-spammer@sha256:" + strings.Repeat("0af6", 16),
		MetricsExporter: "ghcr.io/example/qrl-metrics-exporter@sha256:" + strings.Repeat("0af7", 16),
	}
}

// The Kubernetes rendering of the soak profile is checked in: it is the
// placement and sizing contract with the cluster in qrl-infra, and the file
// doubles as documentation. Regenerate with UPDATE_GOLDEN=1.
func TestSoakParametersKubernetes(t *testing.T) {
	payload, err := resolveParameters(devwallet.Address, StartOptions{
		Images:      soakImages(),
		Profile:     ProfileSoak,
		Backend:     BackendKubernetes,
		LoadPercent: DefaultLoadPercent,
	})
	require.NoError(t, err)

	var pretty bytes.Buffer
	require.NoError(t, json.Indent(&pretty, []byte(payload), "", "  "))
	pretty.WriteByte('\n')

	golden := filepath.Join("testdata", "soak_kubernetes.json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.WriteFile(golden, pretty.Bytes(), 0o600))
	}
	want, err := os.ReadFile(golden)
	require.NoError(t, err)
	require.Equal(t, string(want), pretty.String())

	var parameters map[string]any
	require.NoError(t, json.Unmarshal([]byte(payload), &parameters))
	participants := parameters["participants"].([]any)
	require.Len(t, participants, soakParticipants)
	for index, raw := range participants {
		participant := raw.(map[string]any)
		require.Equal(t, map[string]any{ParticipantNodeLabel: strconv.Itoa(index + 1)}, participant["node_selectors"])
		require.Equal(t, []any{map[string]any{"key": PoolLabel, "operator": "Equal", "value": WorkPool, "effect": "NoSchedule"}}, participant["tolerations"])
		require.Equal(t, participant["el_min_cpu"], participant["el_max_cpu"], "guaranteed QoS needs request == limit")
		require.Equal(t, participant["el_min_mem"], participant["el_max_mem"])
		require.Equal(t, participant["cl_min_mem"], participant["cl_max_mem"])
	}
	require.Equal(t, []any{"prometheus_grafana", "tx_spammer"}, parameters["additional_services"])
	require.Equal(t, true, parameters["qrl_metrics_exporter_enabled"])
	spammer := parameters["tx_spammer_params"].(map[string]any)
	require.Equal(t, soakImages().TxSpammer, spammer["image"])
	require.Equal(t, float64(SoakThroughput(DefaultLoadPercent)), spammer["throughput"])
	network := parameters["network_params"].(map[string]any)
	require.Equal(t, float64(soakGenesisGasLimit), network["genesis_gaslimit"])
	require.Equal(t, float64(soakGenesisDelaySeconds), network["genesis_delay"])
}

func TestSoakParametersParticipantCount(t *testing.T) {
	payload, err := resolveParameters(devwallet.Address, StartOptions{
		Images:           soakImages(),
		Profile:          ProfileSoak,
		Backend:          BackendKubernetes,
		ParticipantCount: 1,
	})
	require.NoError(t, err)
	var parameters map[string]any
	require.NoError(t, json.Unmarshal([]byte(payload), &parameters))
	require.Len(t, parameters["participants"].([]any), 1)
}

func TestSoakParametersDockerAndIdle(t *testing.T) {
	payload, err := resolveParameters(devwallet.Address, StartOptions{
		Images:      soakImages(),
		Profile:     ProfileSoak,
		Backend:     BackendDocker,
		LoadPercent: 0,
	})
	require.NoError(t, err)

	var parameters map[string]any
	require.NoError(t, json.Unmarshal([]byte(payload), &parameters))
	participant := parameters["participants"].([]any)[0].(map[string]any)
	require.NotContains(t, participant, "node_selectors", "Docker has no pools to pin to")
	require.NotContains(t, participant, "tolerations")
	require.NotContains(t, participant, "el_min_cpu")
	require.Equal(t, []any{"prometheus_grafana"}, parameters["additional_services"], "an idle baseline runs no spammer")
	require.NotContains(t, parameters, "tx_spammer_params")
}

func TestSoakThroughput(t *testing.T) {
	require.Equal(t, 0, SoakThroughput(0))
	require.Equal(t, 0, SoakThroughput(-5))
	// 30M gas / 21k per transfer / 5 s slots = 285 TPS at full capacity.
	require.Equal(t, 285, SoakThroughput(100))
	require.Equal(t, 85, SoakThroughput(DefaultLoadPercent))
}

func TestFileParametersPassThroughUnchanged(t *testing.T) {
	address := "Q" + strings.Repeat("b", 128)
	custom := []byte(fmt.Sprintf(`participants:
  - el_image: registry.example/go-qrl:custom
    cl_image: registry.example/qrysm-beacon:custom
    vc_image: registry.example/qrysm-validator:custom
    remote_signer_image: registry.example/clef:custom
    custom: 9007199254740993
network_params:
  prefunded_accounts:
    %s:
      balance: 1QRL
  withdrawal_address: %s
qrl_genesis_generator_params:
  image: registry.example/qrl-genesis:custom
`, address, address))
	rendered, err := resolveParameters(address, StartOptions{Parameters: custom})
	require.NoError(t, err)
	require.Equal(t, string(custom), rendered)

	view := decodedParametersFile(t, rendered)
	require.Equal(t, "registry.example/go-qrl:custom", view.Participants[0].ExecutionImage)
	require.Equal(t, "registry.example/clef:custom", view.Participants[0].RemoteSignerImage)
	require.Equal(t, "registry.example/qrysm-beacon:custom", view.Participants[0].ConsensusImage)
	require.Equal(t, "registry.example/qrysm-validator:custom", view.Participants[0].ValidatorImage)
	require.Equal(t, "registry.example/qrl-genesis:custom", view.Genesis.Image)
	require.Equal(t, "1QRL", view.Network.PrefundedAccounts[address].Balance)
	require.Equal(t, address, view.Network.WithdrawalAddress)
	// 2^53+1: would corrupt to ...992 if pass-through re-encoded via float64.
	require.Equal(t, int64(9007199254740993), view.Participants[0].Custom)
}

func TestFileParametersSupportJSON(t *testing.T) {
	address := "Q" + strings.Repeat("e", 128)
	custom := []byte(fmt.Sprintf(`{
		"participants":[{"el_image":"registry.example/go-qrl:test"}],
		"network_params":{"prefunded_accounts":{"%s":{}}}
	}`, address))
	rendered, err := resolveParameters(address, StartOptions{Parameters: custom})
	require.NoError(t, err)
	require.Equal(t, string(custom), rendered)

	view := decodedParametersFile(t, rendered)
	require.Equal(t, "registry.example/go-qrl:test", view.Participants[0].ExecutionImage)
	require.Contains(t, view.Network.PrefundedAccounts, address)
}

func TestNetworkParametersTemplate(t *testing.T) {
	payload, err := os.ReadFile("network_params.yaml")
	require.NoError(t, err)

	rendered, err := resolveParameters(devwallet.Address, StartOptions{Parameters: payload})
	require.NoError(t, err)
	require.Equal(t, string(payload), rendered)

	view := decodedParametersFile(t, rendered)
	require.Equal(t, DefaultExecutionImage, view.Participants[0].ExecutionImage)
	require.Equal(t, DefaultClefImage, view.Participants[0].RemoteSignerImage)
	require.Equal(t, DefaultConsensusImage, view.Participants[0].ConsensusImage)
	require.Equal(t, DefaultValidatorImage, view.Participants[0].ValidatorImage)
	require.Equal(t, DefaultGenesisImage, view.Genesis.Image)
	require.True(t, view.Participants[0].RemoteSignerAutoApprove)
	require.Contains(t, view.Network.PrefundedAccounts, devwallet.Address)
}

func TestFileParametersRejectInvalid(t *testing.T) {
	address := "Q" + strings.Repeat("c", 128)
	for name, custom := range map[string][]byte{
		"malformed":       []byte(`participants: [`),
		"missing wallet":  []byte("participants:\n  - el_image: image\nnetwork_params:\n  prefunded_accounts: {}\n"),
		"top-level array": []byte(`[]`),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := resolveParameters(address, StartOptions{Parameters: custom})
			require.Error(t, err)
		})
	}
}

type parametersFileView struct {
	Participants []struct {
		ExecutionImage          string `yaml:"el_image"`
		ConsensusImage          string `yaml:"cl_image"`
		ValidatorImage          string `yaml:"vc_image"`
		RemoteSignerImage       string `yaml:"remote_signer_image"`
		RemoteSignerAutoApprove bool   `yaml:"remote_signer_auto_approve"`
		Custom                  int64  `yaml:"custom"`
	} `yaml:"participants"`
	Network struct {
		PrefundedAccounts map[string]struct {
			Balance string `yaml:"balance"`
		} `yaml:"prefunded_accounts"`
		WithdrawalAddress string `yaml:"withdrawal_address"`
	} `yaml:"network_params"`
	Genesis struct {
		Image string `yaml:"image"`
	} `yaml:"qrl_genesis_generator_params"`
}

func decodedParametersFile(t *testing.T, payload string) parametersFileView {
	t.Helper()
	var view parametersFileView
	require.NoError(t, yaml.Unmarshal([]byte(payload), &view))
	return view
}
