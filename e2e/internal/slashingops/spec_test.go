package slashingops

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/stretchr/testify/require"
)

const shorPerQRL = 1_000_000_000

func testSpec() Spec {
	return Spec{
		minSlashingPenaltyQuotient:  32,
		whistleblowerRewardQuotient: 512,
		epochsPerSlashingsVector:    512,
		maxSeedLookahead:            4,
		minWithdrawabilityDelay:     2,
	}
}

func TestLoadSpec(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = fmt.Fprint(writer, `{"data":{
			"MIN_SLASHING_PENALTY_QUOTIENT":"32","WHISTLEBLOWER_REWARD_QUOTIENT":"512",
			"EPOCHS_PER_SLASHINGS_VECTOR":"512","MAX_SEED_LOOKAHEAD":"4",
			"MIN_VALIDATOR_WITHDRAWABILITY_DELAY":"2"}}`)
	}))
	t.Cleanup(server.Close)
	client, err := beacon.New(server.URL)
	require.NoError(t, err)

	spec, err := LoadSpec(t.Context(), client)
	require.NoError(t, err)
	require.Equal(t, testSpec(), spec)
}

func TestSlashingAmounts(t *testing.T) {
	spec := testSpec()
	require.Equal(t, uint64(1_250*shorPerQRL), spec.InitialPenalty(40_000*shorPerQRL))
	require.Equal(t, uint64(78_125_000_000), spec.WhistleblowerReward(40_000*shorPerQRL))
}

func TestSlashingEpochs(t *testing.T) {
	spec := testSpec()
	exit := spec.ExitEpoch(10)
	require.Equal(t, uint64(15), exit)
	// The slashings vector outlasts the withdrawability delay.
	require.Equal(t, uint64(522), spec.WithdrawableEpoch(10, exit))

	spec.minWithdrawabilityDelay = 600
	require.Equal(t, uint64(615), spec.WithdrawableEpoch(10, exit))
}
