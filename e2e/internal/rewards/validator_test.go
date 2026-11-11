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
		minInclusionDelay:            1,
		inactivityScoreBias:          4,
		inactivityScoreRecoveryRate:  16,
		inactivityPenaltyQuotient:    1 << 16,
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

func TestInactivityPenalty(t *testing.T) {
	spec := testSpec()
	require.Zero(t, spec.inactivityPenalty(40_000*shorPerQRL, 0))
	// 40,000 QRL at a score of 4 over 4 × 65,536 is 610,351,562.5 shor,
	// rounded down.
	require.Equal(t, uint64(610_351_562), spec.inactivityPenalty(40_000*shorPerQRL, 4))
	require.Equal(t, uint64(1_830_917_358), spec.inactivityPenalty(39_997*shorPerQRL, 12))
}

func TestInactivityLeakEpochs(t *testing.T) {
	spec := testSpec()
	// With epoch 3 finalized, epoch 7 is four epochs behind, the most
	// without a leak.
	require.Equal(t, uint64(8), spec.FirstLeakEpoch(3))
	require.False(t, spec.inInactivityLeak(7, 3))
	require.True(t, spec.inInactivityLeak(8, 3))
	require.False(t, spec.inInactivityLeak(2, 3))
}

func TestNextInactivityScore(t *testing.T) {
	spec := testSpec()
	for name, test := range map[string]struct {
		score        uint64
		timelyTarget bool
		leak         bool
		want         uint64
	}{
		"missed in a leak":                               {score: 8, leak: true, want: 12},
		"timely in a leak":                               {score: 8, timelyTarget: true, leak: true, want: 7},
		"timely in a leak at zero":                       {score: 0, timelyTarget: true, leak: true, want: 0},
		"missed outside a leak":                          {score: 20, want: 8},
		"missed outside a leak at 0":                     {score: 0, want: 0},
		"timely outside a leak":                          {score: 20, timelyTarget: true, want: 3},
		"timely outside a leak, below the recovery rate": {score: 3, timelyTarget: true, want: 0},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.want, spec.NextInactivityScore(test.score, test.timelyTarget, test.leak))
		})
	}
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
		"WEIGHT_DENOMINATOR":"64","MIN_EPOCHS_TO_INACTIVITY_PENALTY":"4",
		"MIN_ATTESTATION_INCLUSION_DELAY":"1","INACTIVITY_SCORE_BIAS":"4",
		"INACTIVITY_SCORE_RECOVERY_RATE":"16","INACTIVITY_PENALTY_QUOTIENT":"65536"}`})

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

// scoresJSON is a debug state whose validators all have an inactivity score
// of zero but the validator under test, whose score is score.
func scoresJSON(score uint64) string {
	scores := slices.Repeat([]string{`"0"`}, validatorIndex+1)
	scores[validatorIndex] = fmt.Sprintf(`"%d"`, score)
	return `{"inactivity_scores":[` + strings.Join(scores, ",") + `]}`
}

func TestLeakVoteAmounts(t *testing.T) {
	// The rewards of epoch 5 are processed between the states at slots 55
	// and 56. Epoch 0 is still finalized after, so it is a leak, and the
	// processing raises the score from 8 to 12.
	responses := map[string]string{
		"beacon/states/56/finality_checkpoints": finalizedJSON(0),
		"debug/beacon/states/55":                scoresJSON(8),
		"debug/beacon/states/56":                scoresJSON(12),
		"beacon/states/55/validators/64":        validatorJSON(39_997*shorPerQRL, 39_997*shorPerQRL),
		"beacon/states/55/validators":           fmt.Sprintf(`[{"validator":{"effective_balance":"%d"}},{"validator":{"effective_balance":"%d"}}]`, 64*40_000*shorPerQRL, 39_997*shorPerQRL),
		"beacon/rewards/attestations/5":         idealJSON(39_997 * shorPerQRL),
	}
	fake, client := newStateBeacon(t, responses)

	amounts, err := testValidator(client).LeakVoteAmounts(t.Context(), 5)
	require.NoError(t, err)
	require.Equal(t, uint64(351_408_642), amounts.MissedSource)
	require.Equal(t, uint64(652_616_050), amounts.MissedTarget)
	require.True(t, amounts.Leak)
	require.Equal(t, uint64(8), amounts.ScoreBefore)
	require.Equal(t, uint64(12), amounts.ScoreAfter)
	require.Equal(t, uint64(1_830_917_358), amounts.InactivityPenalty)
	for _, path := range fake.paths() {
		require.True(t, strings.Contains(path, "/states/55") || strings.Contains(path, "/states/56") ||
			path == "beacon/rewards/attestations/5", "read %s", path)
	}

	// The processing finalizes epoch 1, which ends the leak.
	responses["beacon/states/56/finality_checkpoints"] = finalizedJSON(1)
	_, client = newStateBeacon(t, responses)
	amounts, err = testValidator(client).LeakVoteAmounts(t.Context(), 5)
	require.NoError(t, err)
	require.False(t, amounts.Leak)

	responses["debug/beacon/states/56"] = `{"inactivity_scores":["0"]}`
	_, client = newStateBeacon(t, responses)
	_, err = testValidator(client).LeakVoteAmounts(t.Context(), 5)
	require.EqualError(t, err, "the state at slot 56 has no inactivity score for validator 64")
}

func TestBalanceAtEpochStart(t *testing.T) {
	_, client := newStateBeacon(t, map[string]string{
		"beacon/states/48/validators/64": validatorJSON(39_999*shorPerQRL, 39_999*shorPerQRL),
	})
	balance, err := testValidator(client).BalanceAtEpochStart(t.Context(), 6)
	require.NoError(t, err)
	require.Equal(t, uint64(39_999*shorPerQRL), balance)
}

func TestRecordAtEpochStart(t *testing.T) {
	_, client := newStateBeacon(t, map[string]string{
		"beacon/states/48/validators/64": validatorJSON(39_999*shorPerQRL, 39_000*shorPerQRL),
	})
	record, err := testValidator(client).RecordAtEpochStart(t.Context(), 6)
	require.NoError(t, err)
	require.Equal(t, uint64(validatorIndex), record.Index)
	require.Equal(t, uint64(39_999*shorPerQRL), record.Balance)
	require.Equal(t, uint64(39_000*shorPerQRL), record.EffectiveBalance)
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
	// BlockBalanceChanges(6) sums slots 49 through 56; 48 and 57 lie outside.
	// Slot 50 is empty, 52 has no sync reward, and the validator proposed 54.
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

func TestTimelyVotes(t *testing.T) {
	// In epoch 5 the validator is member 1 of committee 1, which attests at
	// slot 43. The canonical block at 43 is 0xaa and the epoch's target 0x40.
	header := func(root string) string {
		return fmt.Sprintf(`{"root":"%s","header":{"message":{}}}`, root)
	}
	block := func(head, target, bits string) string {
		return fmt.Sprintf(`{"message":{"body":{"attestations":[{"aggregation_bits":"%s","data":{"slot":"43","index":"1",`+
			`"beacon_block_root":"%s","target":{"epoch":"5","root":"%s"}}}],"execution_payload":{}}}}`, bits, head, target)
	}
	const voted, otherVoted = "0x0a", "0x09" // members 1 and 0 of three
	base := func(blocks map[string]string) map[string]string {
		responses := map[string]string{
			"beacon/states/40/committees": `[{"index":"1","slot":"43","validators":["7","64","9"]}]`,
			"beacon/headers/43":           header("0xaa"),
			"beacon/headers/40":           header("0x40"),
		}
		for path, body := range blocks {
			responses[path] = body
		}
		return responses
	}

	for name, test := range map[string]struct {
		responses map[string]string
		want      TimelyVotes
	}{
		"included in the next slot": {
			base(map[string]string{"beacon/blocks/44": block("0xaa", "0x40", voted)}),
			TimelyVotes{Source: true, Head: true},
		},
		"voted for another head": {
			base(map[string]string{"beacon/blocks/44": block("0xbb", "0x40", voted)}),
			TimelyVotes{Source: true},
		},
		"voted for another target": {
			base(map[string]string{"beacon/blocks/44": block("0xaa", "0xcc", voted)}),
			TimelyVotes{Source: true},
		},
		"included two slots later, after an empty slot": {
			base(map[string]string{"beacon/blocks/45": block("0xaa", "0x40", voted)}),
			TimelyVotes{Source: true},
		},
		"included three slots later": {
			base(map[string]string{"beacon/blocks/46": block("0xaa", "0x40", voted)}),
			TimelyVotes{},
		},
		"only another member voted": {
			base(map[string]string{"beacon/blocks/44": block("0xaa", "0x40", otherVoted)}),
			TimelyVotes{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, client := newStateBeacon(t, test.responses)
			votes, err := testValidator(client).TimelyVotes(t.Context(), 5)
			require.NoError(t, err)
			require.Equal(t, test.want, votes)
		})
	}

	// With slot 43 empty, the canonical head at 43 is the block at 42.
	responses := base(map[string]string{"beacon/blocks/44": block("0x42", "0x40", voted)})
	delete(responses, "beacon/headers/43")
	responses["beacon/headers/42"] = header("0x42")
	_, client := newStateBeacon(t, responses)
	votes, err := testValidator(client).TimelyVotes(t.Context(), 5)
	require.NoError(t, err)
	require.Equal(t, TimelyVotes{Source: true, Head: true}, votes)

	_, client = newStateBeacon(t, map[string]string{
		"beacon/states/40/committees": `[{"index":"1","slot":"43","validators":["7","9"]}]`,
	})
	_, err = testValidator(client).TimelyVotes(t.Context(), 5)
	require.EqualError(t, err, "validator 64 is in no committee of epoch 5")
}
