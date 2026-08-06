package devnet

import (
	"encoding/json"
	"strconv"
)

const (
	defaultNetworkID = "1337"
	prefundBalance   = "2000000QRL"
)

func effectiveParameters(address string, options StartOptions) (string, error) {
	if options.Parameters != nil {
		return customParameters(options.Parameters, address)
	}

	images := options.Images.withDefaults()
	if err := images.validate(); err != nil {
		return "", err
	}

	profile, err := normalizeProfile(options.Profile)
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

// participantParameters keeps a profile's explicit parameter list, including
// an explicitly empty one; only a nil (unset) list falls back to defaults.
func participantParameters(configured []string, defaults ...string) []string {
	if configured != nil {
		return configured
	}
	return append([]string{}, defaults...)
}
