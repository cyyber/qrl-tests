package devnet

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"go.yaml.in/yaml/v3"
)

// DefaultLoadPercent is the share of block gas capacity the soak load
// generator targets unless told otherwise.
const DefaultLoadPercent = 30

// The qrl-package parameter schema, as far as the built-in profiles use it.
type packageParameters struct {
	Participants              []participant     `json:"participants"`
	NetworkParams             networkParams     `json:"network_params"`
	GenesisParams             generatorParams   `json:"qrl_genesis_generator_params"`
	AdditionalServices        []string          `json:"additional_services,omitempty"`
	TxSpammerParams           *txSpammerParams  `json:"tx_spammer_params,omitempty"`
	PrometheusParams          *prometheusParams `json:"prometheus_params,omitempty"`
	QRLMetricsExporterEnabled bool              `json:"qrl_metrics_exporter_enabled,omitempty"`
	GlobalLogLevel            string            `json:"global_log_level,omitempty"`
}

type participant struct {
	ELImage                 string            `json:"el_image"`
	ELExtraParams           []string          `json:"el_extra_params"`
	CLImage                 string            `json:"cl_image"`
	CLExtraParams           []string          `json:"cl_extra_params"`
	VCImage                 string            `json:"vc_image"`
	VCExtraParams           []string          `json:"vc_extra_params"`
	UseRemoteSigner         bool              `json:"use_remote_signer"`
	RemoteSignerType        string            `json:"remote_signer_type"`
	RemoteSignerImage       string            `json:"remote_signer_image"`
	RemoteSignerAutoApprove bool              `json:"remote_signer_auto_approve"`
	ValidatorCount          int               `json:"validator_count"`
	ELExtraLabels           map[string]string `json:"el_extra_labels,omitempty"`
	CLExtraLabels           map[string]string `json:"cl_extra_labels,omitempty"`
	VCExtraLabels           map[string]string `json:"vc_extra_labels,omitempty"`
}

type networkParams struct {
	NetworkID               string             `json:"network_id"`
	PreregisteredValidators int                `json:"preregistered_validator_count,omitempty"`
	SecondsPerSlot          int                `json:"seconds_per_slot"`
	SlotsPerEpoch           int                `json:"slots_per_epoch"`
	GenesisDelay            int                `json:"genesis_delay,omitempty"`
	GenesisGasLimit         int                `json:"genesis_gaslimit,omitempty"`
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

// txSpammerParams drive qrl-package's tx_spammer service. The spammer funds
// itself from the package's own prefunded account, never from the suite
// wallet, so suite specs keep a serial nonce.
type txSpammerParams struct {
	Image      string `json:"image"`
	Scenario   string `json:"scenario"`
	Throughput int    `json:"throughput"`
	MaxPending int    `json:"max_pending"`
	MaxWallets int    `json:"max_wallets"`
}

type prometheusParams struct {
	RetentionTime string `json:"storage_tsdb_retention_time"`
	RetentionSize string `json:"storage_tsdb_retention_size"`
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
	return profileParameters(address, options)
}

func profileParameters(address string, options StartOptions) (string, error) {
	images, err := options.Images.Resolved()
	if err != nil {
		return "", err
	}
	spec := profileSpecs[options.Profile]
	wanted := spec.participants
	if options.ParticipantCount > 0 {
		if options.ParticipantCount > len(wanted) {
			return "", fmt.Errorf("participant count %d exceeds profile %s (%d)", options.ParticipantCount, options.Profile, len(wanted))
		}
		wanted = wanted[:options.ParticipantCount]
	}

	participants := make([]participant, len(wanted))
	for index := range participants {
		configuration := wanted[index]
		labels := map[string]string{
			participantLabel: strconv.Itoa(index + 1),
			// Alternate halves for the network-partition lanes.
			partitionLabel: strconv.Itoa(index%2 + 1),
		}
		participants[index] = participant{
			ELImage:                 images.Execution,
			ELExtraParams:           participantParameters(configuration.elExtraParams, "--graphql", "--graphql.vhosts=*"),
			CLImage:                 images.Consensus,
			CLExtraParams:           participantParameters(configuration.clExtraParams, defaultCLExtraParams...),
			VCImage:                 images.Validator,
			VCExtraParams:           participantParameters(configuration.vcExtraParams),
			UseRemoteSigner:         true,
			RemoteSignerType:        "clef",
			RemoteSignerImage:       images.Clef,
			RemoteSignerAutoApprove: true,
			ValidatorCount:          configuration.validatorCount,
			ELExtraLabels:           labels,
			CLExtraLabels:           labels,
			VCExtraLabels:           labels,
		}
	}

	parameters := packageParameters{
		Participants: participants,
		NetworkParams: networkParams{
			NetworkID:               "1337",
			PreregisteredValidators: spec.preregisteredValidators,
			SecondsPerSlot:          secondsPerSlot,
			SlotsPerEpoch:           8,
			ExecutionFollowDistance: 8,
			WithdrawabilityDelay:    2,
			ShardCommitteePeriod:    2,
			PrefundedAccounts:       map[string]account{address: {Balance: "2000000QRL"}},
			WithdrawalAddress:       address,
			LightKDFEnabled:         true,
		},
		GenesisParams:      generatorParams{Image: images.Genesis},
		AdditionalServices: spec.additionalServices,
	}

	if spec.loadGenerator || spec.metricsExporter {
		// Long-running profiles: nodes must all be scheduled and pulled before
		// genesis, and the gas limit is what load is expressed against.
		parameters.NetworkParams.GenesisDelay = soakGenesisDelaySeconds
		parameters.NetworkParams.GenesisGasLimit = soakGenesisGasLimit
		parameters.PrometheusParams = &prometheusParams{RetentionTime: "2d", RetentionSize: "4GB"}
		parameters.GlobalLogLevel = "info"
	}
	parameters.QRLMetricsExporterEnabled = spec.metricsExporter
	if spec.loadGenerator {
		throughput := SoakThroughput(options.LoadPercent)
		if throughput == 0 {
			// Idle baseline: keep the service list honest.
			parameters.AdditionalServices = withoutService(parameters.AdditionalServices, "tx_spammer")
		} else {
			parameters.TxSpammerParams = &txSpammerParams{
				Image:      images.TxSpammer,
				Scenario:   "eoatx",
				Throughput: throughput,
				MaxPending: throughput * 10,
				MaxWallets: min(500, max(50, throughput*4)),
			}
		}
	}

	payload, err := json.Marshal(parameters)
	if err != nil {
		return "", err
	}

	return string(payload), nil
}

func withoutService(services []string, name string) []string {
	result := make([]string, 0, len(services))
	for _, service := range services {
		if service != name {
			result = append(result, service)
		}
	}
	return result
}

func participantParameters(configured []string, defaults ...string) []string {
	if configured != nil {
		return configured
	}
	return append([]string{}, defaults...)
}

func fileParameters(payload []byte, address string) (string, error) {
	var required requiredParameters
	if err := yaml.Unmarshal(payload, &required); err != nil {
		return "", errors.New("parameters file must contain one YAML mapping")
	}

	if _, ok := required.Network.PrefundedAccounts[address]; !ok {
		return "", fmt.Errorf("network_params.prefunded_accounts must contain development wallet %q", address)
	}

	return string(payload), nil
}
