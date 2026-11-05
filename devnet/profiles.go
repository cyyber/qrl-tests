package devnet

import (
	"fmt"
	"slices"
	"strings"
)

type Profile string

var (
	// defaultCLExtraParams let a beacon node without peers count as synced.
	defaultCLExtraParams = []string{"--min-sync-peers=0", "--minimum-peers-per-subnet=0"}
	anchorCLExtraParams  = append(slices.Clone(defaultCLExtraParams), "--sync-from=head")
	// slasherCLExtraParams run a slasher in the beacon node, which reports a
	// slashable offense to its operation pool. Subscribing to every subnet
	// lets it see every attestation.
	slasherCLExtraParams = append(slices.Clone(defaultCLExtraParams), "--slasher", "--subscribe-all-subnets")
)

// Further profiles arrive together with the lanes that exercise them.
const (
	ProfileSingle Profile = "single"
	// ProfileAnchored has two participants that share the validators. The
	// first is the anchor of a full-outage recovery: its beacon node counts
	// as synced without peers and starts from its stored head. By default a
	// beacon node starts from the justified checkpoint, and a network whose
	// beacon nodes all stopped never proposes again. The second keeps the
	// package's sync settings, so it follows the anchor.
	ProfileAnchored Profile = "anchored"
	// ProfileSpareValidator is the single profile with one more genesis
	// validator than its participant runs. No client signs for the spare
	// until a suite starts one with its key, derived from the pinned
	// qrl-package's default mnemonic.
	ProfileSpareValidator Profile = "spare-validator"
	// ProfileDuplicateValidator has two participants that share the
	// validators, each beacon node with a slasher, and one more genesis
	// validator than they run: a spare that a suite can run on both nodes at
	// once, as an operator who duplicates a validator would.
	ProfileDuplicateValidator Profile = "duplicate-validator"
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
	ProfileAnchored: {participants: []participantSpec{
		{validatorCount: 32, clExtraParams: anchorCLExtraParams},
		{validatorCount: 32, clExtraParams: []string{"--minimum-peers-per-subnet=0"}},
	}},
	ProfileSpareValidator: {
		participants:            []participantSpec{{validatorCount: 64}},
		preregisteredValidators: 65,
	},
	ProfileDuplicateValidator: {
		participants: []participantSpec{
			{validatorCount: 32, clExtraParams: slasherCLExtraParams},
			{validatorCount: 32, clExtraParams: slasherCLExtraParams},
		},
		preregisteredValidators: 65,
	},
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

// ParticipantValidators returns the genesis indices of the validators the
// participant at position, from zero, runs. qrl-package hands each
// participant the next indices in order.
func (profile Profile) ParticipantValidators(position int) []uint64 {
	spec := profileSpecs[profile]
	if position < 0 || position >= len(spec.participants) {
		return nil
	}
	var first int
	for _, participant := range spec.participants[:position] {
		first += participant.validatorCount
	}
	indices := make([]uint64, spec.participants[position].validatorCount)
	for offset := range indices {
		indices[offset] = uint64(first + offset)
	}
	return indices
}

// SpareValidators returns the genesis indices of the profile's validators
// that no participant runs. qrl-package hands the participants the first
// indices in order, so the spares follow them.
func (profile Profile) SpareValidators() []uint64 {
	spec := profileSpecs[profile]
	var assigned int
	for _, participant := range spec.participants {
		assigned += participant.validatorCount
	}
	var spares []uint64
	for index := assigned; index < spec.preregisteredValidators; index++ {
		spares = append(spares, uint64(index))
	}
	return spares
}
