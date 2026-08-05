package devnet

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	packageLocator   = "github.com/rgeraldes24/qrl-package@3892c3d2596403c080424d9e8fc99ff172483fe0"
	defaultNetworkID = "1337"
	prefundBalance   = "2000000QRL"
	engineJWTSecret  = "0xdc49981516e8e72b401a63e6405495a32dafc3939b5d6d83cc319ac0388bca1b"

	rpcPortID           = "rpc"
	webSocketPortID     = "ws"
	consensusHTTPPortID = "http"
	metricsPortID       = "metrics"
	graphQLPath         = "/graphql"
)

const (
	DefaultExecutionImage = "local/go-qrl:devnet"
	DefaultClefImage      = "local/go-qrl-clef:devnet"
	DefaultConsensusImage = "qrledger/qrysm:beacon-chain-8b80fa0c3f5a"
	DefaultValidatorImage = "qrledger/qrysm:validator-8b80fa0c3f5a"
	DefaultGenesisImage   = "qrledger/qrysm:qrl-genesis-generator-360410c72353-8b80fa0c3f5a"
)

type parameterShape struct {
	Participants []struct {
		ExecutionImage    string `json:"el_image" yaml:"el_image"`
		ConsensusImage    string `json:"cl_image" yaml:"cl_image"`
		ValidatorImage    string `json:"vc_image" yaml:"vc_image"`
		RemoteSignerImage string `json:"remote_signer_image" yaml:"remote_signer_image"`
	} `json:"participants" yaml:"participants"`
	Network struct {
		PrefundedAccounts map[string]any `json:"prefunded_accounts" yaml:"prefunded_accounts"`
	} `json:"network_params" yaml:"network_params"`
	Genesis struct {
		Image string `json:"image" yaml:"image"`
	} `json:"qrl_genesis_generator_params" yaml:"qrl_genesis_generator_params"`
}

// The qrl-package parameter schema, as far as the built-in profile uses it.
type packageParameters struct {
	Participants  []participant   `json:"participants"`
	NetworkParams networkParams   `json:"network_params"`
	GenesisParams generatorParams `json:"qrl_genesis_generator_params"`
}

type participant struct {
	ELImage           string            `json:"el_image"`
	ELExtraParams     []string          `json:"el_extra_params"`
	CLImage           string            `json:"cl_image"`
	CLExtraParams     []string          `json:"cl_extra_params"`
	VCImage           string            `json:"vc_image"`
	VCExtraParams     []string          `json:"vc_extra_params"`
	UseRemoteSigner   bool              `json:"use_remote_signer"`
	RemoteSignerType  string            `json:"remote_signer_type"`
	RemoteSignerImage string            `json:"remote_signer_image"`
	ValidatorCount    int               `json:"validator_count"`
	ELExtraLabels     map[string]string `json:"el_extra_labels,omitempty"`
	CLExtraLabels     map[string]string `json:"cl_extra_labels,omitempty"`
	VCExtraLabels     map[string]string `json:"vc_extra_labels,omitempty"`
}

type networkParams struct {
	NetworkID               string             `json:"network_id"`
	PreregisteredValidators int                `json:"preregistered_validator_count,omitempty"`
	SecondsPerSlot          int                `json:"seconds_per_slot"`
	SlotsPerEpoch           int                `json:"slots_per_epoch"`
	ExecutionFollowDistance int                `json:"execution_follow_distance"`
	WithdrawabilityDelay    int                `json:"min_validator_withdrawability_delay"`
	ShardCommitteePeriod    int                `json:"shard_committee_period"`
	PrefundedAccounts       map[string]account `json:"prefunded_accounts"`
	WithdrawalAddress       string             `json:"withdrawal_address"`
	LightKDFEnabled         bool               `json:"light_kdf_enabled"`
}

type account struct {
	Balance string `json:"balance"`
}

type generatorParams struct {
	Image string `json:"image"`
}

func effectiveParametersForProfile(address string, images Images, custom []byte, profile Profile, backend Backend) (string, error) {
	if custom != nil {
		return customParameters(custom, address, backend)
	}
	images = images.withDefaults()
	if err := images.validate(backend); err != nil {
		return "", err
	}
	profile, err := normalizeProfile(profile)
	if err != nil {
		return "", err
	}
	spec := profileSpecs[profile]
	participants := make([]participant, len(spec.participants))
	for index := range participants {
		configuration := spec.participants[index]
		labels := map[string]string{
			"qrl-tests.participant": strconv.Itoa(index + 1),
			"qrl-tests.partition":   strconv.Itoa(index%2 + 1),
		}
		participants[index] = participant{
			ELImage:           images.Execution,
			ELExtraParams:     participantParameters(configuration.elExtraParams, "--graphql", "--graphql.vhosts=*"),
			CLImage:           images.Consensus,
			CLExtraParams:     participantParameters(configuration.clExtraParams, "--min-sync-peers=0", "--minimum-peers-per-subnet=0"),
			VCImage:           images.Validator,
			VCExtraParams:     participantParameters(configuration.vcExtraParams),
			UseRemoteSigner:   true,
			RemoteSignerType:  "clef",
			RemoteSignerImage: images.Clef,
			ValidatorCount:    configuration.validatorCount,
			ELExtraLabels:     labels,
			CLExtraLabels:     labels,
			VCExtraLabels:     labels,
		}
	}
	payload, err := json.Marshal(packageParameters{
		Participants: participants,
		NetworkParams: networkParams{
			NetworkID:               defaultNetworkID,
			PreregisteredValidators: spec.preregisteredValidators,
			SecondsPerSlot:          5,
			SlotsPerEpoch:           8,
			ExecutionFollowDistance: 8,
			WithdrawabilityDelay:    2,
			ShardCommitteePeriod:    2,
			PrefundedAccounts:       map[string]account{address: {Balance: prefundBalance}},
			WithdrawalAddress:       address,
			LightKDFEnabled:         true,
		},
		GenesisParams: generatorParams{Image: images.Genesis},
	})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func participantParameters(configured []string, defaults ...string) []string {
	if configured != nil {
		return configured
	}
	return append([]string{}, defaults...)
}

func customParameters(payload []byte, address string, backend Backend) (string, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(payload, &document); err != nil {
		return "", errors.New("parameters file must contain one YAML mapping")
	}
	shape, err := decodeParameterShape(&document)
	if err != nil {
		return "", err
	}
	if len(shape.Participants) == 0 || strings.TrimSpace(shape.Participants[0].ExecutionImage) == "" {
		return "", errors.New("first participant el_image must be set")
	}
	if _, ok := shape.Network.PrefundedAccounts[address]; !ok {
		return "", fmt.Errorf("network_params.prefunded_accounts must contain development wallet %q", address)
	}
	if err := shape.validateImages(backend); err != nil {
		return "", err
	}
	return string(payload), nil
}

func decodeParameterShape(document *yaml.Node) (parameterShape, error) {
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return parameterShape{}, errors.New("parameters file must contain one YAML mapping")
	}
	var shape parameterShape
	if err := document.Decode(&shape); err != nil {
		return parameterShape{}, errors.New("parameters file must contain one YAML mapping")
	}
	return shape, nil
}

func (shape parameterShape) validateImages(backend Backend) error {
	if backend != BackendKubernetes {
		return nil
	}
	for index, participant := range shape.Participants {
		for _, item := range []struct {
			name, image string
		}{
			{"execution", participant.ExecutionImage},
			{"Clef", participant.RemoteSignerImage},
			{"consensus", participant.ConsensusImage},
			{"validator", participant.ValidatorImage},
		} {
			if strings.HasPrefix(strings.TrimSpace(item.image), "local/") {
				return fmt.Errorf("participant %d %s image %q is not available to Kubernetes; use a registry image", index+1, item.name, item.image)
			}
		}
	}
	if strings.HasPrefix(strings.TrimSpace(shape.Genesis.Image), "local/") {
		return fmt.Errorf("genesis image %q is not available to Kubernetes; use a registry image", shape.Genesis.Image)
	}
	return nil
}

type Profile string

const (
	ProfileSingle        Profile = "single"
	ProfileMulti         Profile = "multi"
	ProfileChaos         Profile = "chaos"
	ProfileSync          Profile = "sync"
	ProfileOperations    Profile = "operations"
	ProfileCold          Profile = "cold"
	ProfileOptimistic    Profile = "optimistic"
	ProfileExecutionSync Profile = "execution-sync"
)

type profileSpec struct {
	participants            []participantSpec
	preregisteredValidators int
}

type participantSpec struct {
	validatorCount int
	elExtraParams  []string
	clExtraParams  []string
	vcExtraParams  []string
}

var profileSpecs = map[Profile]profileSpec{
	ProfileSingle: {participants: []participantSpec{{validatorCount: 64}}},
	ProfileMulti:  {participants: []participantSpec{{validatorCount: 16}, {validatorCount: 16}, {validatorCount: 16}, {validatorCount: 16}}},
	ProfileChaos: {
		participants: []participantSpec{
			{validatorCount: 16, clExtraParams: []string{}},
			{validatorCount: 16, clExtraParams: []string{}},
			{validatorCount: 16, clExtraParams: []string{}},
			{validatorCount: 16, clExtraParams: []string{}},
		},
	},
	ProfileSync: {
		participants: []participantSpec{
			{validatorCount: 32},
			{
				validatorCount: 32,
				clExtraParams:  []string{"--min-sync-peers=0", "--minimum-peers-per-subnet=0", "--force-clear-db"},
				vcExtraParams:  []string{"--enable-doppelganger", "--force-clear-db"},
			},
		},
	},
	ProfileOperations: {
		participants: []participantSpec{
			{validatorCount: 128},
			{validatorCount: 128},
			{validatorCount: 128},
			{validatorCount: 128},
			{validatorCount: 300},
		},
		preregisteredValidators: 512,
	},
	ProfileCold: {
		participants: []participantSpec{{
			validatorCount: 64,
			clExtraParams:  []string{"--min-sync-peers=0", "--minimum-peers-per-subnet=0", "--slots-per-archive-point=16"},
		}},
	},
	ProfileOptimistic: {
		participants: []participantSpec{
			{validatorCount: 32},
			{
				validatorCount: 32,
				clExtraParams:  []string{"--min-sync-peers=0", "--minimum-peers-per-subnet=0", "--startup-optimistic"},
			},
		},
	},
	ProfileExecutionSync: {
		participants: []participantSpec{
			{validatorCount: 64},
			{
				validatorCount: 0,
				elExtraParams:  []string{"--graphql", "--graphql.vhosts=*", "--nodiscover", "--bootnodes="},
			},
		},
	},
}

func normalizeProfile(profile Profile) (Profile, error) {
	if profile == "" {
		return ProfileSingle, nil
	}
	if _, exists := profileSpecs[profile]; !exists {
		return "", fmt.Errorf("unknown development-network profile %q", profile)
	}
	return profile, nil
}
