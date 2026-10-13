package devnet

import (
	"fmt"
	"slices"
	"strings"
)

type Profile string

var (
	// defaultCLExtraParams let a beacon node without peers count as synced.
	defaultCLExtraParams      = []string{"--min-sync-peers=0", "--minimum-peers-per-subnet=0"}
	syncFromHeadCLExtraParams = append(slices.Clone(defaultCLExtraParams), "--sync-from=head")
)

// Further profiles arrive together with the lanes that exercise them.
const (
	ProfileSingle Profile = "single"
	// ProfileMulti has two participants that share the validators. Their
	// beacon nodes start from the stored head: by default a beacon node
	// starts from the justified checkpoint, and a network whose beacon nodes
	// all stopped never proposes again.
	ProfileMulti Profile = "multi"
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
	ProfileMulti: {participants: []participantSpec{
		{validatorCount: 32, clExtraParams: syncFromHeadCLExtraParams},
		{validatorCount: 32, clExtraParams: syncFromHeadCLExtraParams},
	}},
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
