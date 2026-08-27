package devnet

import (
	"fmt"
	"strings"
)

type Profile string

// Further profiles arrive together with the lanes that exercise them.
const ProfileSingle Profile = "single"

// NetworkExpectations are fixed values established by a built-in profile.
type NetworkExpectations struct {
	ChainID            string `json:"chain_id"`
	NetworkID          string `json:"network_id"`
	ExecutionPeerCount int    `json:"execution_peer_count"`
}

type profileSpec struct {
	participants            []participantSpec
	preregisteredValidators int
	expectations            NetworkExpectations
}

type participantSpec struct {
	validatorCount int
	elExtraParams  []string
	clExtraParams  []string
	vcExtraParams  []string
}

var profileSpecs = map[Profile]profileSpec{
	ProfileSingle: {
		participants: []participantSpec{{validatorCount: 64}},
		expectations: NetworkExpectations{
			ChainID:            "0x539",
			NetworkID:          "1337",
			ExecutionPeerCount: 0,
		},
	},
}

// Expectations returns the fixed values established by this built-in profile.
func (profile Profile) Expectations() (NetworkExpectations, bool) {
	spec, found := profileSpecs[profile]
	return spec.expectations, found
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
