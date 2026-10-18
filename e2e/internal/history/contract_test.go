package history

import (
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/core/vm"
	"github.com/theQRL/go-qrl/crypto"
	"github.com/theQRL/go-qrl/params"
)

func TestFillerSetsSlotsAndLogs(t *testing.T) {
	qrvm, state := newQRVM()
	sender := vm.AccountRef(common.Address{0x01})
	code, address, _, err := qrvm.Create(sender, fillerInitCode(), math.MaxUint64, new(big.Int))
	require.NoError(t, err)
	require.Equal(t, fillerRuntime(), code)

	_, _, err = qrvm.Call(sender, address, fillCalldata(1000, 3), math.MaxUint64, new(big.Int))
	require.NoError(t, err)

	for slot := uint64(1000); slot < 1003; slot++ {
		want := common.BytesToStorageValue64(new(big.Int).SetUint64(slot + 1).Bytes())
		require.Equal(t, want, state.GetState(address, slotKey(slot)), "slot %d", slot)
	}
	require.Equal(t, common.StorageValue64{}, state.GetState(address, slotKey(1003)), "the slot after the last is untouched")
	require.Equal(t, common.StorageValue64{}, state.GetState(address, slotKey(999)), "the slot before the first is untouched")

	require.Len(t, state.logs, 1)
	require.Equal(t, address, state.logs[0].Address)
	require.Len(t, state.logs[0].Topics, 1)
	var topic common.LogTopic
	big.NewInt(1000).FillBytes(topic[:])
	require.Equal(t, topic, state.logs[0].Topics[0])
	require.Empty(t, state.logs[0].Data)
}

func TestFillerWithNoSlots(t *testing.T) {
	qrvm, state := newQRVM()
	sender := vm.AccountRef(common.Address{0x01})
	_, address, _, err := qrvm.Create(sender, fillerInitCode(), math.MaxUint64, new(big.Int))
	require.NoError(t, err)

	_, _, err = qrvm.Call(sender, address, fillCalldata(5, 0), math.MaxUint64, new(big.Int))
	require.NoError(t, err)
	require.Equal(t, common.StorageValue64{}, state.GetState(address, slotKey(5)))
	require.Len(t, state.logs, 1)
}

func newQRVM() (*vm.QRVM, *memoryState) {
	state := newMemoryState()
	blockContext := vm.BlockContext{
		CanTransfer: func(vm.StateDB, common.Address, *big.Int) bool { return true },
		Transfer:    func(vm.StateDB, common.Address, common.Address, *big.Int) {},
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
		BlockNumber: new(big.Int),
		BaseFee:     new(big.Int),
		Random:      &common.Hash{},
	}
	txContext := vm.TxContext{GasPrice: new(big.Int)}
	chainConfig := &params.ChainConfig{ChainID: big.NewInt(1)}
	return vm.NewQRVM(blockContext, txContext, state, chainConfig, vm.Config{}), state
}

// memoryState is the part of a state database the filler needs: accounts
// without balances, code, storage and logs. go-qrl's own in-memory state does
// not build against this module's dependencies.
type memoryState struct {
	vm.StateDB
	nonces  map[common.Address]uint64
	code    map[common.Address][]byte
	storage map[common.Address]map[common.Hash]common.StorageValue64
	logs    []*types.Log
}

func newMemoryState() *memoryState {
	return &memoryState{
		nonces:  map[common.Address]uint64{},
		code:    map[common.Address][]byte{},
		storage: map[common.Address]map[common.Hash]common.StorageValue64{},
	}
}

func (state *memoryState) CreateAccount(common.Address)              {}
func (state *memoryState) GetBalance(common.Address) *big.Int        { return new(big.Int) }
func (state *memoryState) GetNonce(address common.Address) uint64    { return state.nonces[address] }
func (state *memoryState) SetNonce(address common.Address, n uint64) { state.nonces[address] = n }
func (state *memoryState) GetCode(address common.Address) []byte     { return state.code[address] }
func (state *memoryState) SetCode(address common.Address, code []byte) {
	state.code[address] = code
}
func (state *memoryState) GetCodeSize(address common.Address) int { return len(state.code[address]) }

func (state *memoryState) GetCodeHash(address common.Address) common.Hash {
	if code := state.code[address]; len(code) > 0 {
		return crypto.Keccak256Hash(code)
	}
	return common.Hash{}
}

func (state *memoryState) GetState(address common.Address, key common.Hash) common.StorageValue64 {
	return state.storage[address][key]
}

func (state *memoryState) GetCommittedState(address common.Address, key common.Hash) common.StorageValue64 {
	return common.StorageValue64{}
}

func (state *memoryState) SetState(address common.Address, key common.Hash, value common.StorageValue64) {
	if state.storage[address] == nil {
		state.storage[address] = map[common.Hash]common.StorageValue64{}
	}
	state.storage[address][key] = value
}

func (state *memoryState) Exist(address common.Address) bool {
	_, exists := state.nonces[address]
	return exists || len(state.code[address]) > 0
}

func (state *memoryState) Empty(address common.Address) bool { return !state.Exist(address) }
func (state *memoryState) AddRefund(uint64)                  {}
func (state *memoryState) SubRefund(uint64)                  {}
func (state *memoryState) GetRefund() uint64                 { return 0 }
func (state *memoryState) AddressInAccessList(common.Address) bool {
	return true
}

func (state *memoryState) SlotInAccessList(common.Address, common.Hash) (bool, bool) {
	return true, true
}
func (state *memoryState) AddAddressToAccessList(common.Address)           {}
func (state *memoryState) AddSlotToAccessList(common.Address, common.Hash) {}
func (state *memoryState) Snapshot() int                                   { return 0 }
func (state *memoryState) RevertToSnapshot(int)                            {}
func (state *memoryState) AddLog(log *types.Log)                           { state.logs = append(state.logs, log) }
func (state *memoryState) AddPreimage(common.Hash, []byte)                 {}
