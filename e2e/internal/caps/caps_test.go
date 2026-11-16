package caps

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCheck(t *testing.T) {
	limit := Cap{Name: "test", Limit: 1000}
	for _, test := range []struct {
		name     string
		class    Class
		observed uint64
		verdict  Verdict
	}{
		{"a worst case with a tenth to spare", WorstCase, 900, Pass},
		{"a worst case with less than a tenth to spare", WorstCase, 901, Tight},
		{"a worst case at the cap", WorstCase, 1000, Tight},
		{"a worst case above the cap", WorstCase, 1001, Fail},

		{"a measured size with a tenth to spare", Measured, 900, Pass},
		{"a measured size with less than a tenth to spare", Measured, 901, Fail},
		{"a measured size above the cap", Measured, 1001, Fail},

		{"a duration at half of the budget", TimeBudget, 500, Pass},
		{"a duration above half of the budget", TimeBudget, 501, Fail},
		{"a duration above the budget", TimeBudget, 1001, Fail},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := limit.Check(test.class, test.observed)
			require.Equal(t, test.verdict, result.Verdict)
			require.Equal(t, limit, result.Cap)
			require.Equal(t, test.observed, result.Observed)
		})
	}
}

func TestCheckDuration(t *testing.T) {
	require.Equal(t, Pass, EnginePayload.CheckDuration(4*time.Second).Verdict)
	require.Equal(t, Fail, EnginePayload.CheckDuration(4*time.Second+time.Millisecond).Verdict)
	// A clock that went backwards is no time at all.
	require.Equal(t, Pass, EnginePayload.CheckDuration(-time.Second).Verdict)
}

func TestSpare(t *testing.T) {
	limit := Cap{Name: "test", Limit: 1000}
	require.InDelta(t, 0.25, limit.Check(Measured, 750).Spare(), 1e-9)
	require.InDelta(t, -0.5, limit.Check(Measured, 1500).Spare(), 1e-9)
}

func TestResultString(t *testing.T) {
	require.Equal(t,
		"beacon gossip message: 9238235 of 10485760 bytes, 11.9% to spare: pass",
		BeaconGossip.Check(Measured, 9238235).String())
	require.Equal(t,
		"qrl protocol message: 10485761 of 10485760 bytes, above the cap: fail",
		QRLMessage.Check(WorstCase, 10485761).String())
	require.Equal(t,
		"engine API payload call: 1.5s of 8s, 81.2% to spare: pass",
		EnginePayload.CheckDuration(1500*time.Millisecond).String())
}

func TestMinBandwidth(t *testing.T) {
	limit := Cap{Name: "test", Limit: uint64(20 * time.Second)}
	require.InDelta(t, 4.0, limit.MinBandwidth(10_000_000), 1e-9, "10 MB in 20 s needs 4 Mbit/s")
}
