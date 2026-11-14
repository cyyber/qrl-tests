package load

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/params"
)

func TestBlockFull(t *testing.T) {
	for _, test := range []struct {
		name    string
		gasUsed uint64
		full    bool
	}{
		{"at the gas limit", 20_000_000, true},
		{"one transfer short of the gas limit", 20_000_000 - params.TxGas, true},
		{"at the full share", 19_000_000, true},
		{"just under the full share", 18_999_999, false},
		{"empty", 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.full, Block{GasUsed: test.gasUsed, GasLimit: 20_000_000}.Full())
		})
	}
}

func TestExtend(t *testing.T) {
	full := Block{GasUsed: 20_000_000, GasLimit: 20_000_000}
	partial := Block{GasUsed: 10_000_000, GasLimit: 20_000_000}

	var run []Block
	for number, block := range []Block{full, full, partial, full} {
		block.Number = uint64(number)
		run = extend(run, block)
	}
	// The block that is not full starts the run over.
	require.Len(t, run, 1)
	require.Equal(t, uint64(3), run[0].Number)
}

func TestFeeCapFor(t *testing.T) {
	tip := big.NewInt(1)

	// The fee cap is above the base fee that many full blocks lead to.
	baseFee := big.NewInt(1_000_000_000)
	const blocks = 10
	feeCap, err := feeCapFor(baseFee, tip, blocks)
	require.NoError(t, err)
	reached := new(big.Int).Set(baseFee)
	for range blocks {
		reached.Add(reached, new(big.Int).Div(reached, big.NewInt(params.DefaultBaseFeeChangeDenominator)))
	}
	require.Equal(t, 1, feeCap.Cmp(reached), "fee cap %s, base fee after %d full blocks %s", feeCap, blocks, reached)

	// A base fee too low for an eighth of it to count still rises by one.
	feeCap, err = feeCapFor(big.NewInt(7), tip, blocks)
	require.NoError(t, err)
	require.Equal(t, 1, feeCap.Cmp(big.NewInt(7+blocks)))

	// A transfer cannot pay more than the node accepts over the API.
	_, err = feeCapFor(baseFee, tip, 200)
	require.ErrorIs(t, err, ErrFeeCap)
}
