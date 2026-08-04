// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

package devnet

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltInProfiles(t *testing.T) {
	address := "Q" + strings.Repeat("d", 128)
	for _, test := range []struct {
		profile      Profile
		participants int
		validators   int
	}{
		{ProfileSingle, 1, 64},
		{ProfileMulti, 4, 64},
		{ProfileChaos, 4, 64},
		{ProfileSync, 2, 64},
		{ProfileOperations, 5, 812},
		{ProfileCold, 1, 64},
		{ProfileOptimistic, 2, 64},
		{ProfileExecutionSync, 2, 64},
	} {
		payload, err := effectiveParametersForProfile(address, Images{Execution: "image"}.withDefaults(), nil, test.profile)
		require.NoError(t, err)
		var parameters struct {
			Participants []struct {
				ValidatorCount int      `json:"validator_count"`
				ELExtraParams  []string `json:"el_extra_params"`
				CLExtraParams  []string `json:"cl_extra_params"`
				VCExtraParams  []string `json:"vc_extra_params"`
			} `json:"participants"`
			Network struct {
				PreregisteredValidators int `json:"preregistered_validator_count"`
			} `json:"network_params"`
		}
		require.NoError(t, json.Unmarshal([]byte(payload), &parameters))
		require.Len(t, parameters.Participants, test.participants)
		totalValidators := 0
		for _, participant := range parameters.Participants {
			totalValidators += participant.ValidatorCount
			require.NotNil(t, participant.ELExtraParams)
			require.NotNil(t, participant.CLExtraParams)
			require.NotNil(t, participant.VCExtraParams)
		}
		require.Equal(t, test.validators, totalValidators)
		if test.profile == ProfileSync {
			require.Contains(t, parameters.Participants[1].CLExtraParams, "--force-clear-db")
			require.Equal(t, []string{"--enable-doppelganger", "--force-clear-db"}, parameters.Participants[1].VCExtraParams)
		}
		if test.profile == ProfileChaos {
			require.Empty(t, parameters.Participants[0].CLExtraParams)
			require.NotNil(t, parameters.Participants[0].CLExtraParams)
		}
		if test.profile == ProfileCold {
			require.Contains(t, parameters.Participants[0].CLExtraParams, "--slots-per-archive-point=16")
		}
		if test.profile == ProfileOptimistic {
			require.Contains(t, parameters.Participants[1].CLExtraParams, "--startup-optimistic")
		}
		if test.profile == ProfileOperations {
			require.Equal(t, 512, parameters.Network.PreregisteredValidators)
			require.Equal(t, 300, parameters.Participants[4].ValidatorCount)
		}
		if test.profile == ProfileExecutionSync {
			require.Zero(t, parameters.Participants[1].ValidatorCount)
			require.Contains(t, parameters.Participants[1].ELExtraParams, "--nodiscover")
			require.Contains(t, parameters.Participants[1].ELExtraParams, "--bootnodes=")
			require.Contains(t, parameters.Participants[1].CLExtraParams, "--min-sync-peers=0")
		}
	}
}
