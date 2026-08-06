package devnet

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
	DefaultConsensusImage = "local/qrysm-beacon:devnet"
	DefaultValidatorImage = "local/qrysm-validator:devnet"
	DefaultGenesisImage   = "local/qrl-genesis-generator:devnet"
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
