package consolefixture

import (
	_ "embed"
	"strings"

	"github.com/theQRL/go-qrl/common/hexutil"
)

// ABI is the generated contract ABI used by the console suite.
//
//go:embed testdata/ConsoleFixture.abi
var ABI []byte

//go:embed testdata/ConsoleFixture.bin
var bytecode string

// Bytecode decodes the generated contract bytecode used by the console suite.
func Bytecode() ([]byte, error) {
	return hexutil.Decode("0x" + strings.TrimPrefix(strings.TrimSpace(bytecode), "0x"))
}
