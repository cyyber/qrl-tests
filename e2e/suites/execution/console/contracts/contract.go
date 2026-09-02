// Package contracts contains the contracts used by the execution console suite.
package contracts

import (
	_ "embed"
	"strings"

	"github.com/theQRL/go-qrl/common/hexutil"
)

// ConsoleProbeABI is the generated contract ABI used by the console suite.
//
//go:embed ConsoleProbe.abi
var ConsoleProbeABI []byte

//go:embed ConsoleProbe.bin
var consoleProbeBytecode string

// ConsoleProbeBytecode decodes the generated contract bytecode used by the
// console suite.
func ConsoleProbeBytecode() ([]byte, error) {
	return hexutil.Decode("0x" + strings.TrimPrefix(strings.TrimSpace(consoleProbeBytecode), "0x"))
}
