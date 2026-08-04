// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

package devnet

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestDefaultParameters(t *testing.T) {
	address := "Q" + strings.Repeat("a", 128)
	const executionImage = "local/go-qrl:test"
	payload, err := testParameters(address, executionImage, nil)
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
	require.Equal(t, DefaultGenesisImage, parameters["qrl_genesis_generator_params"].(map[string]any)["image"])
	require.Equal(t, "1337", network["network_id"])
	require.Equal(t, address, network["withdrawal_address"])
	require.Equal(t, prefundBalance, prefund["balance"])
	require.Regexp(t, `^github\.com/rgeraldes24/qrl-package@[0-9a-f]{40}$`, packageLocator)
}

func TestCustomParameterTokens(t *testing.T) {
	address := "Q" + strings.Repeat("b", 128)
	custom := []byte(`participants:
  - el_image: __DEVNET_EXECUTION_IMAGE__
    cl_image: __DEVNET_CONSENSUS_IMAGE__
    vc_image: __DEVNET_VALIDATOR_IMAGE__
    remote_signer_image: __DEVNET_CLEF_IMAGE__
    custom: 9007199254740993
network_params:
  prefunded_accounts:
    __DEVNET_WALLET_ADDRESS__:
      balance: 1QRL
  withdrawal_address: __DEVNET_WALLET_ADDRESS__
untouched: prefix-__DEVNET_EXECUTION_IMAGE__
qrl_genesis_generator_params:
  image: __DEVNET_GENESIS_IMAGE__
`)
	rendered, err := testParameters(address, "registry.example/qrl:test", custom)
	require.NoError(t, err)
	require.Contains(t, rendered, `custom: 9007199254740993`)
	require.Contains(t, rendered, `el_image: registry.example/qrl:test`)
	require.Contains(t, rendered, `untouched: prefix-__DEVNET_EXECUTION_IMAGE__`)
	shape := decodedParameterShape(t, rendered)
	require.Equal(t, "registry.example/qrl:test", shape.Participants[0].ExecutionImage)
	require.Equal(t, DefaultClefImage, shape.Participants[0].RemoteSignerImage)
	require.Equal(t, DefaultConsensusImage, shape.Participants[0].ConsensusImage)
	require.Equal(t, DefaultValidatorImage, shape.Participants[0].ValidatorImage)
	require.Equal(t, DefaultGenesisImage, shape.Genesis.Image)
	require.Contains(t, shape.Network.PrefundedAccounts, address)
}

func TestCustomJSONParametersRemainSupported(t *testing.T) {
	address := "Q" + strings.Repeat("e", 128)
	custom := []byte(`{
		"participants":[{"el_image":"__DEVNET_EXECUTION_IMAGE__"}],
		"network_params":{"prefunded_accounts":{"__DEVNET_WALLET_ADDRESS__":{}}}
	}`)
	rendered, err := testParameters(address, "image", custom)
	require.NoError(t, err)
	shape := decodedParameterShape(t, rendered)
	require.Equal(t, "image", shape.Participants[0].ExecutionImage)
	require.Contains(t, shape.Network.PrefundedAccounts, address)
}

func TestNetworkParametersTemplate(t *testing.T) {
	address := "Q" + strings.Repeat("f", 128)
	payload, err := os.ReadFile("network_params.yaml")
	require.NoError(t, err)

	rendered, err := testParameters(address, "local/go-qrl:test", payload)
	require.NoError(t, err)
	shape := decodedParameterShape(t, rendered)
	require.Equal(t, "local/go-qrl:test", shape.Participants[0].ExecutionImage)
	require.Equal(t, DefaultClefImage, shape.Participants[0].RemoteSignerImage)
	require.Contains(t, shape.Network.PrefundedAccounts, address)
}

func TestInvalidCustomParameters(t *testing.T) {
	address := "Q" + strings.Repeat("c", 128)
	for name, custom := range map[string][]byte{
		"malformed":       []byte(`participants: [`),
		"missing image":   []byte("participants:\n  - el_image: image\nnetwork_params:\n  prefunded_accounts:\n    __DEVNET_WALLET_ADDRESS__: {}\n"),
		"missing wallet":  []byte("participants:\n  - el_image: __DEVNET_EXECUTION_IMAGE__\nnetwork_params:\n  prefunded_accounts: {}\n"),
		"top-level array": []byte(`[]`),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := testParameters(address, "image", custom)
			require.Error(t, err)
		})
	}
}

func testParameters(address, executionImage string, custom []byte) (string, error) {
	images := DefaultImages()
	images.Execution = executionImage
	return effectiveParametersForProfile(address, images, custom, ProfileSingle)
}

func decodedParameterShape(t *testing.T, payload string) parameterShape {
	t.Helper()
	var document yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(payload), &document))
	shape, err := decodeParameterShape(&document)
	require.NoError(t, err)
	return shape
}
