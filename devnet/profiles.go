package devnet

import (
	"fmt"
	"strings"
)

type Profile string

const (
	// ProfileSingle is one participant: the functional E2E lanes.
	ProfileSingle Profile = "single"
	// ProfileSoak is a multi-participant network under sustained load with
	// metrics collection, sized so that on Kubernetes every participant owns
	// one work node.
	ProfileSoak Profile = "soak"
)

type profileSpec struct {
	participants            []participantSpec
	preregisteredValidators int

	// additionalServices are qrl-package extras launched alongside the
	// participants.
	additionalServices []string
	// loadGenerator enables the transaction spammer; its throughput follows
	// StartOptions.LoadPercent.
	loadGenerator bool
	// metricsExporter runs qrl-metrics-exporter next to every participant.
	metricsExporter bool
	// pinnedPlacement places participant N on the node labelled
	// qrl.io/participant=N with guaranteed resources when the backend is
	// Kubernetes. Docker ignores it.
	pinnedPlacement bool
	resources       *participantResources
}

type participantSpec struct {
	validatorCount int
	elExtraParams  []string
	clExtraParams  []string
	vcExtraParams  []string
}

// participantResources are per-client CPU (millicores) and memory (MB)
// requests; the soak profile sets limits equal to requests so the kubelet
// grants guaranteed QoS and nothing under test is throttled or evicted.
type participantResources struct {
	elCPU, elMemory int
	clCPU, clMemory int
	vcCPU, vcMemory int
}

const (
	soakParticipants        = 4
	soakValidatorsPerNode   = 64
	secondsPerSlot      = 5
	soakGenesisGasLimit     = 30_000_000
	soakSimpleTransferGas   = 21_000
	soakGenesisDelaySeconds = 120
)

// Sized for an 8 vCPU / 32 GB participant node with room for the kubelet,
// the CNI and the logs collector.
var soakResources = participantResources{
	elCPU: 3000, elMemory: 12288,
	clCPU: 3000, clMemory: 10240,
	vcCPU: 500, vcMemory: 1024,
}

var profileSpecs = map[Profile]profileSpec{
	ProfileSingle: {participants: []participantSpec{{validatorCount: 64}}},
	ProfileSoak: {
		participants:       soakParticipantSpecs(),
		additionalServices: []string{"prometheus_grafana", "tx_spammer"},
		loadGenerator:      true,
		metricsExporter:    true,
		pinnedPlacement:    true,
		resources:          &soakResources,
	},
}

func soakParticipantSpecs() []participantSpec {
	specs := make([]participantSpec, soakParticipants)
	for index := range specs {
		specs[index] = participantSpec{validatorCount: soakValidatorsPerNode}
	}
	return specs
}

// ParseProfile validates the raw value, resolving the empty value to the
// default single-participant profile; only a verified value becomes a Profile.
func ParseProfile(value string) (Profile, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ProfileSingle, nil
	}

	profile := Profile(trimmed)
	if _, exists := profileSpecs[profile]; !exists {
		return "", fmt.Errorf("unknown development-network profile %q", value)
	}
	return profile, nil
}

// ParticipantCount reports how many participants the profile provisions.
func (profile Profile) ParticipantCount() int {
	return len(profileSpecs[profile].participants)
}

// SoakThroughput converts a share of block gas capacity into the spammer's
// transactions-per-second target: how many simple transfers fit in a block,
// spread over a slot. 30% of a 30M gas block at 5 s slots is ~85 TPS.
func SoakThroughput(loadPercent int) int {
	if loadPercent <= 0 {
		return 0
	}
	perSecond := float64(soakGenesisGasLimit) / soakSimpleTransferGas / secondsPerSlot
	return int(perSecond * float64(loadPercent) / 100)
}
