// Package contracts contains the contracts used by the execution console suite.
package contracts

import (
	_ "embed"
	"strings"

	"github.com/theQRL/go-qrl/common/hexutil"
)

// Artifact contains a generated contract ABI and its encoded bytecode.
type Artifact struct {
	ABI         []byte
	bytecodeHex string
}

//go:embed ConsoleProbe.abi
var consoleProbeABI []byte

//go:embed ConsoleProbe.bin
var consoleProbeBytecode string

// ConsoleProbe exercises contract interactions through the console.
var ConsoleProbe = Artifact{ABI: consoleProbeABI, bytecodeHex: consoleProbeBytecode}

// Bytecode decodes the artifact's generated contract bytecode.
func (artifact Artifact) Bytecode() ([]byte, error) {
	return hexutil.Decode("0x" + strings.TrimPrefix(strings.TrimSpace(artifact.bytecodeHex), "0x"))
}
