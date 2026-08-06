package devnet

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"go.yaml.in/yaml/v3"
)

const (
	defaultNetworkID = "1337"
	prefundBalance   = "2000000QRL"
)

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

// The invariant every custom parameter file must satisfy: the development
// wallet driving readiness probes and suites must be prefunded. The rest of
// the file passes through to qrl-package unvalidated; JSON files decode
// through the same YAML path.
type requiredParameters struct {
	Network struct {
		PrefundedAccounts map[string]any `yaml:"prefunded_accounts"`
	} `yaml:"network_params"`
}

func resolveParameters(address string, options StartOptions) (string, error) {
	if options.Parameters != nil {
		return fileParameters(options.Parameters, address)
	}

	images := options.Images.withDefaults()
	if err := images.validate(); err != nil {
		return "", err
	}

	spec, ok := profileSpecs[options.Profile]
	if !ok {
		return "", fmt.Errorf("unknown development-network profile %q", options.Profile)
	}
	participants := make([]participant, len(spec.participants))
	for index := range participants {
		configuration := spec.participants[index]
		labels := map[string]string{
			participantLabel: strconv.Itoa(index + 1),
			partitionLabel:   strconv.Itoa(index%2 + 1),
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

// participantParameters keeps a profile's explicit parameter list, including
// an explicitly empty one; only a nil (unset) list falls back to defaults.
func participantParameters(configured []string, defaults ...string) []string {
	if configured != nil {
		return configured
	}
	return append([]string{}, defaults...)
}

func fileParameters(payload []byte, address string) (string, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(payload, &document); err != nil {
		return "", errors.New("parameters file must contain one YAML mapping")
	}
	required, err := decodeRequiredParameters(&document)
	if err != nil {
		return "", err
	}

	if _, ok := required.Network.PrefundedAccounts[address]; !ok {
		return "", fmt.Errorf("network_params.prefunded_accounts must contain development wallet %q", address)
	}

	return string(payload), nil
}

func decodeRequiredParameters(document *yaml.Node) (requiredParameters, error) {
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return requiredParameters{}, errors.New("parameters file must contain one YAML mapping")
	}
	var required requiredParameters
	if err := document.Decode(&required); err != nil {
		return requiredParameters{}, errors.New("parameters file must contain one YAML mapping")
	}
	return required, nil
}
