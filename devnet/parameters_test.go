package devnet

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cyyber/qrl-tests/internal/devwallet"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestDefaultParameters(t *testing.T) {
	address := "Q" + strings.Repeat("a", 128)
	const executionImage = "local/go-qrl:test"
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
	require.Equal(t, float64(64), participant["validator_count"])
	require.Equal(t, []any{"--graphql", "--graphql.vhosts=*"}, participant["el_extra_params"])
	require.Equal(t, []any{"--min-sync-peers=0", "--minimum-peers-per-subnet=0"}, participant["cl_extra_params"])
	require.Equal(t, []any{}, participant["vc_extra_params"])
	require.Equal(t, DefaultGenesisImage, parameters["qrl_genesis_generator_params"].(map[string]any)["image"])
	require.Equal(t, "1337", network["network_id"])
	require.Equal(t, address, network["withdrawal_address"])
	require.Equal(t, "2000000QRL", prefund["balance"])
}

func TestProfileParametersCarryDigestReferences(t *testing.T) {
	address := "Q" + strings.Repeat("d", 128)
	digested := "ghcr.io/example/go-qrl@sha256:" + strings.Repeat("0af1", 16)
	payload, err := resolveParameters(address, StartOptions{
		Images:  Images{Execution: digested},
		Profile: ProfileSingle,
	})
	require.NoError(t, err)

	view := decodedParametersFile(t, payload)
	require.Equal(t, digested, view.Participants[0].ExecutionImage)
}

func TestProfileParametersRejectInvalidImages(t *testing.T) {
	address := "Q" + strings.Repeat("f", 128)
	_, err := resolveParameters(address, StartOptions{
		Images:  Images{Execution: "local/go qrl:devnet"},
		Profile: ProfileSingle,
	})
	require.ErrorContains(t, err, "execution image")
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
		ExecutionImage    string `yaml:"el_image"`
		ConsensusImage    string `yaml:"cl_image"`
		ValidatorImage    string `yaml:"vc_image"`
		RemoteSignerImage string `yaml:"remote_signer_image"`
		Custom            int64  `yaml:"custom"`
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
