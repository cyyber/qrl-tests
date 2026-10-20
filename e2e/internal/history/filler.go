package history

import (
	_ "embed"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/theQRL/go-qrl/accounts/abi"
	"github.com/theQRL/go-qrl/common"
)

// Regenerate the source-controlled Hyperion artifacts.
// The compiler must be cyyber/hyperion@2b9a0f1d.
//
//go:generate sh -c "hypc --version 2>&1 | grep -Fq commit.2b9a0f1d || { echo 'hypc from cyyber/hyperion@2b9a0f1d is required; found:' >&2; hypc --version >&2; exit 1; }"
//go:generate hypc --abi --bin --no-cbor-metadata --overwrite -o . Filler.hyp

//go:embed Filler.abi
var fillerABI string

//go:embed Filler.bin
var fillerBin string

func fillerBytecode() ([]byte, error) {
	return hex.DecodeString(strings.TrimSpace(fillerBin))
}

func fillCalldata(start, count uint64) ([]byte, error) {
	parsed, err := abi.JSON(strings.NewReader(fillerABI))
	if err != nil {
		return nil, fmt.Errorf("parse Filler ABI: %w", err)
	}
	return parsed.Pack("fill", new(big.Int).SetUint64(start), new(big.Int).SetUint64(count))
}

func slotKey(slot uint64) common.Hash {
	return common.BigToHash(new(big.Int).SetUint64(slot))
}
