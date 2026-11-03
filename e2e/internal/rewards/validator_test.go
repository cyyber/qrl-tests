package rewards

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/stretchr/testify/require"
)

const shorPerQRL = 1_000_000_000

func testSpec() Spec {
	return Spec{
		increment:                    shorPerQRL,
		baseRewardFactor:             2048,
		sourceWeight:                 14,
		targetWeight:                 26,
		weightDenominator:            64,
		minEpochsToInactivityPenalty: 4,
	}
}

func TestMissedVotePenalties(t *testing.T) {
	spec := testSpec()

	// 65 validators at 40,000 QRL: the square root of the total active
	// balance is 50,990,195, so each increment's base reward is 40,164 shor.
	source, target := spec.missedVotePenalties(40_000*shorPerQRL, 65*40_000*shorPerQRL)
	require.Equal(t, uint64(351_435_000), source)
	require.Equal(t, uint64(652_665_000), target)

	// 39,997.5 QRL counts as 39,997 whole increments, and the total active
	// balance includes the validator's own 39,997 QRL.
	source, target = spec.missedVotePenalties(39_997*shorPerQRL+shorPerQRL/2, 64*40_000*shorPerQRL+39_997*shorPerQRL)
	require.Equal(t, uint64(351_408_642), source)
	require.Equal(t, uint64(652_616_050), target)
}

// stateBeacon serves canned data per path and records every path requested.
type stateBeacon struct {
	mu        sync.Mutex
	responses map[string]string
	requested []string
}

func newStateBeacon(t *testing.T, responses map[string]string) (*stateBeacon, *beacon.Client) {
	t.Helper()
	fake := &stateBeacon{responses: responses}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	client, err := beacon.New(server.URL)
	require.NoError(t, err)
	return fake, client
}

func (fake *stateBeacon) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	path := strings.TrimPrefix(request.URL.Path, "/qrl/v1/")
	fake.requested = append(fake.requested, path)
	data, ok := fake.responses[path]
	if !ok {
		http.NotFound(writer, request)
		return
	}
	_, _ = fmt.Fprintf(writer, `{"data":%s}`, data)
}

func (fake *stateBeacon) paths() []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return slices.Clone(fake.requested)
}

func testValidator(client *beacon.Client) Validator {
	return Validator{
		Client: client,
		Chain:  chaininfo.Info{SlotsPerEpoch: 8},
		Spec:   testSpec(),
		Index:  validatorIndex,
	}
}

func validatorJSON(balance, effectiveBalance uint64) string {
	return fmt.Sprintf(`{"index":"%d","balance":"%d","validator":{"effective_balance":"%d"}}`,
		validatorIndex, balance, effectiveBalance)
}

func finalizedJSON(epoch uint64) string {
	return fmt.Sprintf(`{"finalized":{"epoch":"%d","root":"0x00"}}`, epoch)
}

func TestLoadSpec(t *testing.T) {
	_, client := newStateBeacon(t, map[string]string{"config/spec": `{
		"EFFECTIVE_BALANCE_INCREMENT":"1000000000",
		"BASE_REWARD_FACTOR":"2048","TIMELY_SOURCE_WEIGHT":"14","TIMELY_TARGET_WEIGHT":"26",
		"WEIGHT_DENOMINATOR":"64","MIN_EPOCHS_TO_INACTIVITY_PENALTY":"4"}`})

	spec, err := LoadSpec(t.Context(), client)
	require.NoError(t, err)
	require.Equal(t, testSpec(), spec)
}

func idealJSON(effectiveBalances ...uint64) string {
	var rewards []string
	for _, balance := range effectiveBalances {
		rewards = append(rewards, fmt.Sprintf(`{"effective_balance":"%d","head":"%d","target":"%d","source":"%d"}`,
			balance, balance/100_000, balance/50_000, balance/100_000))
	}
	return `{"ideal_rewards":[` + strings.Join(rewards, ",") + `],"total_rewards":[]}`
}

func TestVoteAmounts(t *testing.T) {
	// The rewards of epoch 5 are processed in the state at slot 55, the last
	// of epoch 6.
	responses := map[string]string{
		"beacon/states/55/finality_checkpoints": finalizedJSON(3),
		"beacon/states/55/validators/64":        validatorJSON(39_997*shorPerQRL, 39_997*shorPerQRL),
		"beacon/states/55/validators":           fmt.Sprintf(`[{"validator":{"effective_balance":"%d"}},{"validator":{"effective_balance":"%d"}}]`, 64*40_000*shorPerQRL, 39_997*shorPerQRL),
		"beacon/rewards/attestations/5":         idealJSON(39_997*shorPerQRL, 40_000*shorPerQRL),
	}
	fake, client := newStateBeacon(t, responses)

	amounts, err := testValidator(client).VoteAmounts(t.Context(), 5)
	require.NoError(t, err)
	require.Equal(t, uint64(351_408_642), amounts.MissedSource)
	require.Equal(t, uint64(652_616_050), amounts.MissedTarget)
	// The ideal reward is the one at the validator's own effective balance.
	require.Equal(t, beacon.IdealAttestationReward{
		EffectiveBalance: 39_997 * shorPerQRL,
		Head:             39_997 * shorPerQRL / 100_000,
		Target:           39_997 * shorPerQRL / 50_000,
		Source:           39_997 * shorPerQRL / 100_000,
	}, amounts.Timely)
	for _, path := range fake.paths() {
		require.True(t, strings.HasPrefix(path, "beacon/states/55/") || path == "beacon/rewards/attestations/5",
			"read %s", path)
	}
}

func TestVoteAmountsNeedsIdealReward(t *testing.T) {
	_, client := newStateBeacon(t, map[string]string{
		"beacon/states/55/finality_checkpoints": finalizedJSON(3),
		"beacon/states/55/validators/64":        validatorJSON(39_997*shorPerQRL, 39_997*shorPerQRL),
		"beacon/states/55/validators":           fmt.Sprintf(`[{"validator":{"effective_balance":"%d"}}]`, 65*40_000*shorPerQRL),
		"beacon/rewards/attestations/5":         idealJSON(40_000 * shorPerQRL),
	})
	_, err := testValidator(client).VoteAmounts(t.Context(), 5)
	require.EqualError(t, err, "epoch 5 has no ideal reward for effective balance 39997000000000")
}

func TestVoteAmountsRefusesLeak(t *testing.T) {
	// Epoch 5 is four epochs past finalized epoch 1, the most without a leak.
	_, client := newStateBeacon(t, map[string]string{
		"beacon/states/55/finality_checkpoints": finalizedJSON(1),
		"beacon/states/55/validators/64":        validatorJSON(40_000*shorPerQRL, 40_000*shorPerQRL),
		"beacon/states/55/validators":           fmt.Sprintf(`[{"validator":{"effective_balance":"%d"}}]`, 65*40_000*shorPerQRL),
		"beacon/rewards/attestations/5":         idealJSON(40_000 * shorPerQRL),
	})
	_, err := testValidator(client).VoteAmounts(t.Context(), 5)
	require.NoError(t, err)

	// Five epochs past finalized epoch 0 is a leak.
	_, client = newStateBeacon(t, map[string]string{"beacon/states/55/finality_checkpoints": finalizedJSON(0)})
	_, err = testValidator(client).VoteAmounts(t.Context(), 5)
	require.EqualError(t, err,
		"the rewards of epoch 5 are processed in an inactivity leak: the last finalized epoch is 0")
}

func TestBalanceAtEpochStart(t *testing.T) {
	_, client := newStateBeacon(t, map[string]string{
		"beacon/states/48/validators/64": validatorJSON(39_999*shorPerQRL, 39_999*shorPerQRL),
	})
	balance, err := testValidator(client).BalanceAtEpochStart(t.Context(), 6)
	require.NoError(t, err)
	require.Equal(t, uint64(39_999*shorPerQRL), balance)
}

func TestBlockBalanceChanges(t *testing.T) {
	block := func(withdrawals string) string {
		return fmt.Sprintf(`{"message":{"proposer_index":"3","body":{"execution_payload":{"withdrawals":[%s]}}}}`, withdrawals)
	}
	withdrawal := func(index, amount uint64) string {
		return fmt.Sprintf(`{"validator_index":"%d","amount":"%d"}`, index, amount)
	}
	syncReward := func(reward int64) string {
		return fmt.Sprintf(`[{"validator_index":"%d","reward":"%d"}]`, validatorIndex, reward)
	}
	// BlockBalanceChanges(6) covers slots 49 through 56. Slots 48 and 57
	// belong to the balances either side, slot 50 is empty, at slot 52 the
	// validator is outside the sync committee, and it proposed the block at
	// slot 54.
	responses := map[string]string{
		"beacon/blocks/48":                 block(withdrawal(validatorIndex, 1000)),
		"beacon/blocks/49":                 block(withdrawal(validatorIndex, 7) + "," + withdrawal(3, 500)),
		"beacon/rewards/sync_committee/49": syncReward(-2),
		"beacon/blocks/51":                 block(""),
		"beacon/rewards/sync_committee/51": syncReward(5),
		"beacon/blocks/52":                 block(""),
		"beacon/rewards/sync_committee/52": `[]`,
		"beacon/blocks/56":                 block(""),
		"beacon/rewards/sync_committee/56": syncReward(-3),
		"beacon/blocks/57":                 block(withdrawal(validatorIndex, 1000)),
	}
	for slot := 53; slot <= 55; slot++ {
		responses[fmt.Sprintf("beacon/blocks/%d", slot)] = block("")
		responses[fmt.Sprintf("beacon/rewards/sync_committee/%d", slot)] = `[]`
	}
	responses["beacon/blocks/54"] = fmt.Sprintf(
		`{"message":{"proposer_index":"%d","body":{"execution_payload":{"withdrawals":[]}}}}`, validatorIndex)
	responses["beacon/rewards/blocks/54"] = fmt.Sprintf(`{"proposer_index":"%d","total":"40"}`, validatorIndex)
	fake, client := newStateBeacon(t, responses)

	change, err := testValidator(client).BlockBalanceChanges(t.Context(), 6)
	require.NoError(t, err)
	require.Equal(t, int64(-7-2+5+40-3), change)
	// Only the block the validator proposed is asked for its rewards.
	for _, path := range fake.paths() {
		if strings.HasPrefix(path, "beacon/rewards/blocks/") {
			require.Equal(t, "beacon/rewards/blocks/54", path)
		}
	}
	require.NotContains(t, fake.paths(), "beacon/blocks/48")
	require.NotContains(t, fake.paths(), "beacon/blocks/57")
	require.NotContains(t, fake.paths(), "beacon/rewards/sync_committee/50")
}

func TestBlockBalanceChangesFailedRead(t *testing.T) {
	_, client := newStateBeacon(t, map[string]string{
		"beacon/blocks/49": `{"message":{"body":{"execution_payload":{}}}}`,
	})
	_, err := testValidator(client).BlockBalanceChanges(t.Context(), 6)
	require.ErrorContains(t, err, "sync_committee/49")
}
