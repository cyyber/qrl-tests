package history

import (
	"math/big"

	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/vm"
)

// fillerRuntime is the code of a contract that a call with the words start
// and count makes set storage slots start to start+count-1, each to its key
// plus one, then log start as its only topic. It is written in QRVM opcodes
// rather than compiled, so the history needs no compiler: words are 64 bytes
// and the DUP, SWAP and LOG opcodes are numbered differently from the EVM's.
//
// Stack comments list the stack from the bottom up.
func fillerRuntime() []byte {
	const (
		loop = 0x09
		done = 0x1d
	)
	code := []byte{
		byte(vm.PUSH1), 0x00, byte(vm.CALLDATALOAD), // [start]
		byte(vm.DUP1),                                       // [start, start]
		byte(vm.PUSH1), vm.WordBytes, byte(vm.CALLDATALOAD), // [start, start, count]
		byte(vm.ADD),   // [start, end]
		byte(vm.SWAP1), // [end, slot]
		// loop:
		byte(vm.JUMPDEST),
		byte(vm.DUP2), byte(vm.DUP2), // [end, slot, end, slot]
		byte(vm.LT), byte(vm.ISZERO), // [end, slot, slot >= end]
		byte(vm.PUSH1), done, byte(vm.JUMPI), // [end, slot]
		byte(vm.DUP1), byte(vm.PUSH1), 0x01, byte(vm.ADD), // [end, slot, slot+1]
		byte(vm.DUP2),                      // [end, slot, slot+1, slot]
		byte(vm.SSTORE),                    // [end, slot]
		byte(vm.PUSH1), 0x01, byte(vm.ADD), // [end, slot+1]
		byte(vm.PUSH1), loop, byte(vm.JUMP),
		// done:
		byte(vm.JUMPDEST),
		byte(vm.PUSH1), 0x00, byte(vm.CALLDATALOAD), // [end, slot, start]
		byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, // [end, slot, start, size, offset]
		byte(vm.LOG1),
		byte(vm.STOP),
	}
	if code[loop] != byte(vm.JUMPDEST) || code[done] != byte(vm.JUMPDEST) {
		panic("filler jump destinations moved")
	}
	return code
}

// fillerInitCode deploys fillerRuntime: it copies the runtime that follows
// it into memory and returns it.
func fillerInitCode() []byte {
	runtime := fillerRuntime()
	const prefixLength = 11
	initCode := []byte{
		byte(vm.PUSH1), byte(len(runtime)), // [size]
		byte(vm.DUP1),                // [size, size]
		byte(vm.PUSH1), prefixLength, // [size, size, offset]
		byte(vm.PUSH1), 0x00, // [size, size, offset, destination]
		byte(vm.CODECOPY),    // [size]
		byte(vm.PUSH1), 0x00, // [size, offset]
		byte(vm.RETURN),
	}
	if len(initCode) != prefixLength {
		panic("filler init code length changed")
	}
	return append(initCode, runtime...)
}

// fillCalldata is the input of a call that fills count slots from start.
func fillCalldata(start, count uint64) []byte {
	input := make([]byte, 2*vm.WordBytes)
	new(big.Int).SetUint64(start).FillBytes(input[:vm.WordBytes])
	new(big.Int).SetUint64(count).FillBytes(input[vm.WordBytes:])
	return input
}

// slotKey is the storage key the filler writes for slot.
func slotKey(slot uint64) common.Hash {
	return common.BigToHash(new(big.Int).SetUint64(slot))
}
