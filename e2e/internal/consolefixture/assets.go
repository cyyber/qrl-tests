// Package consolefixture owns the Hyperion contract artifacts used by the
// embedded web3 console suite.
package consolefixture

import (
	_ "embed"
	"strings"

	"github.com/theQRL/go-qrl/common/hexutil"
)

// Regenerate the source-controlled Hyperion artifacts.
// The compiler must be cyyber/hyperion@2b9a0f1d.
//
//go:generate sh -c "hypc --version 2>&1 | grep -Fq commit.2b9a0f1d || { echo 'hypc from cyyber/hyperion@2b9a0f1d is required; found:' >&2; hypc --version >&2; exit 1; }"
//go:generate hypc --abi --bin --no-cbor-metadata --overwrite -o testdata testdata/ConsoleFixture.hyp

//go:embed testdata/ConsoleFixture.abi
var ABI []byte

//go:embed testdata/ConsoleFixture.bin
var bytecode string

func Bytecode() ([]byte, error) {
	return hexutil.Decode("0x" + strings.TrimPrefix(strings.TrimSpace(bytecode), "0x"))
}
