// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

package devnet

import "fmt"

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
