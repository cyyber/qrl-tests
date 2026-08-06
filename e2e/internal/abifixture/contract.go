// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package abifixture

import (
	"errors"
	"math/big"
	"strings"

	qrl "github.com/theQRL/go-qrl"
	"github.com/theQRL/go-qrl/accounts/abi"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = qrl.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// EventEmitterBoundaryEdges is an auto generated low-level Go binding around an user-defined struct.
type EventEmitterBoundaryEdges struct {
	Unsigned248  *big.Int
	Signed248    *big.Int
	Unsigned256  *big.Int
	Signed256    *big.Int
	Unsigned264  *big.Int
	Signed264    *big.Int
	Unsigned504  *big.Int
	Signed504    *big.Int
	Unsigned512  *big.Int
	Signed512    *big.Int
	Bytes31Value [31]byte
	Bytes32Value [32]byte
	Bytes33Value [33]byte
	Bytes63Value [63]byte
	Bytes64Value [64]byte
}

// EventEmitterDynamicRecord is an auto generated low-level Go binding around an user-defined struct.
type EventEmitterDynamicRecord struct {
	Amount  *big.Int
	Note    string
	Payload []byte
	Values  [][]uint16
}

// EventEmitterFunctionRecord is an auto generated low-level Go binding around an user-defined struct.
type EventEmitterFunctionRecord struct {
	Callback [68]byte
	Note     string
}

// EventEmitterNestedRecord is an auto generated low-level Go binding around an user-defined struct.
type EventEmitterNestedRecord struct {
	FixedRecord   EventEmitterRecord
	DynamicRecord EventEmitterDynamicRecord
	Extra         []byte
}

// EventEmitterRecord is an auto generated low-level Go binding around an user-defined struct.
type EventEmitterRecord struct {
	Amount    *big.Int
	Recipient common.Address
	Tag       [64]byte
}

// EventEmitterMetaData contains all meta data concerning the EventEmitter contract.
var EventEmitterMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"uint512\",\"name\":\"initial\",\"type\":\"uint512\"},{\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"bytes64\",\"name\":\"tag\",\"type\":\"bytes64\"}],\"internalType\":\"structEventEmitter.Record\",\"name\":\"record\",\"type\":\"tuple\"},{\"internalType\":\"uint16[]\",\"name\":\"numbers\",\"type\":\"uint16[]\"}],\"stateMutability\":\"payable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"uint512\",\"name\":\"code\",\"type\":\"uint512\"},{\"internalType\":\"string\",\"name\":\"reason\",\"type\":\"string\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"bytes64\",\"name\":\"tag\",\"type\":\"bytes64\"}],\"internalType\":\"structEventEmitter.Record\",\"name\":\"record\",\"type\":\"tuple\"},{\"internalType\":\"uint16[][]\",\"name\":\"nested\",\"type\":\"uint16[][]\"}],\"name\":\"ComplexFailure\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"Halted\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"internalType\":\"uint16[][]\",\"name\":\"values\",\"type\":\"uint16[][]\"}],\"indexed\":false,\"internalType\":\"structEventEmitter.DynamicRecord\",\"name\":\"record\",\"type\":\"tuple\"},{\"indexed\":false,\"internalType\":\"uint16[3]\",\"name\":\"fixedNumbers\",\"type\":\"uint16[3]\"},{\"indexed\":false,\"internalType\":\"string[2]\",\"name\":\"fixedStrings\",\"type\":\"string[2]\"},{\"indexed\":false,\"internalType\":\"uint16[][2]\",\"name\":\"mixed\",\"type\":\"uint16[][2]\"}],\"name\":\"Composite\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint512\",\"name\":\"value\",\"type\":\"uint512\"},{\"indexed\":false,\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"bytes64\",\"name\":\"tag\",\"type\":\"bytes64\"}],\"indexed\":false,\"internalType\":\"structEventEmitter.Record\",\"name\":\"record\",\"type\":\"tuple\"},{\"indexed\":false,\"internalType\":\"uint16[]\",\"name\":\"numbers\",\"type\":\"uint16[]\"}],\"name\":\"Deployed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"indexed\":true,\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"indexed\":false,\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"}],\"name\":\"Dynamic\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"FallbackCalled\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"function(uint512)pureexternalreturns(uint512)\",\"name\":\"indexedCallback\",\"type\":\"function\"},{\"indexed\":false,\"internalType\":\"function(uint512)pureexternalreturns(uint512)\",\"name\":\"callback\",\"type\":\"function\"},{\"indexed\":false,\"internalType\":\"uint512\",\"name\":\"result\",\"type\":\"uint512\"}],\"name\":\"FunctionObserved\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bool\",\"name\":\"flag\",\"type\":\"bool\"},{\"indexed\":true,\"internalType\":\"bytes5\",\"name\":\"code\",\"type\":\"bytes5\"},{\"indexed\":true,\"internalType\":\"int16\",\"name\":\"delta\",\"type\":\"int16\"}],\"name\":\"IndexedScalars\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"uint16\",\"name\":\"marker\",\"type\":\"uint16\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"Paid\",\"type\":\"event\"},{\"anonymous\":true,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint16\",\"name\":\"marker\",\"type\":\"uint16\"},{\"indexed\":false,\"internalType\":\"uint512\",\"name\":\"value\",\"type\":\"uint512\"}],\"name\":\"Pinged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"Received\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"bytes64\",\"name\":\"tag\",\"type\":\"bytes64\"}],\"indexed\":true,\"internalType\":\"structEventEmitter.Record\",\"name\":\"record\",\"type\":\"tuple\"}],\"name\":\"RecordSeen\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"indexed\":true,\"internalType\":\"int512\",\"name\":\"delta\",\"type\":\"int512\"},{\"indexed\":false,\"internalType\":\"bytes64\",\"name\":\"tag\",\"type\":\"bytes64\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"indexed\":false,\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"enabled\",\"type\":\"bool\"}],\"name\":\"Stored\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint16\",\"name\":\"value\",\"type\":\"uint16\"}],\"name\":\"Transformed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"string\",\"name\":\"value\",\"type\":\"string\"}],\"name\":\"Transformed\",\"type\":\"event\"},{\"stateMutability\":\"payable\",\"type\":\"fallback\"},{\"inputs\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"int512\",\"name\":\"delta\",\"type\":\"int512\"},{\"internalType\":\"bytes64\",\"name\":\"tag\",\"type\":\"bytes64\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"internalType\":\"bool\",\"name\":\"enabled\",\"type\":\"bool\"}],\"name\":\"echo\",\"outputs\":[{\"internalType\":\"uint512\",\"name\":\"\",\"type\":\"uint512\"},{\"internalType\":\"int512\",\"name\":\"\",\"type\":\"int512\"},{\"internalType\":\"bytes64\",\"name\":\"\",\"type\":\"bytes64\"},{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"\",\"type\":\"bytes\"},{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"},{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint8\",\"name\":\"smallUnsigned\",\"type\":\"uint8\"},{\"internalType\":\"int8\",\"name\":\"smallSigned\",\"type\":\"int8\"},{\"internalType\":\"uint256\",\"name\":\"wideUnsigned\",\"type\":\"uint256\"},{\"internalType\":\"int256\",\"name\":\"wideSigned\",\"type\":\"int256\"},{\"internalType\":\"bytes5\",\"name\":\"shortBytes\",\"type\":\"bytes5\"},{\"internalType\":\"uint16[3]\",\"name\":\"fixedNumbers\",\"type\":\"uint16[3]\"},{\"internalType\":\"string[2]\",\"name\":\"fixedStrings\",\"type\":\"string[2]\"},{\"internalType\":\"uint16[][2]\",\"name\":\"mixed\",\"type\":\"uint16[][2]\"}],\"name\":\"echoBoundaries\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"},{\"internalType\":\"int8\",\"name\":\"\",\"type\":\"int8\"},{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"},{\"internalType\":\"int256\",\"name\":\"\",\"type\":\"int256\"},{\"internalType\":\"bytes5\",\"name\":\"\",\"type\":\"bytes5\"},{\"internalType\":\"uint16[3]\",\"name\":\"\",\"type\":\"uint16[3]\"},{\"internalType\":\"string[2]\",\"name\":\"\",\"type\":\"string[2]\"},{\"internalType\":\"uint16[][2]\",\"name\":\"\",\"type\":\"uint16[][2]\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint248\",\"name\":\"unsigned248\",\"type\":\"uint248\"},{\"internalType\":\"int248\",\"name\":\"signed248\",\"type\":\"int248\"},{\"internalType\":\"uint256\",\"name\":\"unsigned256\",\"type\":\"uint256\"},{\"internalType\":\"int256\",\"name\":\"signed256\",\"type\":\"int256\"},{\"internalType\":\"uint264\",\"name\":\"unsigned264\",\"type\":\"uint264\"},{\"internalType\":\"int264\",\"name\":\"signed264\",\"type\":\"int264\"},{\"internalType\":\"uint504\",\"name\":\"unsigned504\",\"type\":\"uint504\"},{\"internalType\":\"int504\",\"name\":\"signed504\",\"type\":\"int504\"},{\"internalType\":\"uint512\",\"name\":\"unsigned512\",\"type\":\"uint512\"},{\"internalType\":\"int512\",\"name\":\"signed512\",\"type\":\"int512\"},{\"internalType\":\"bytes31\",\"name\":\"bytes31Value\",\"type\":\"bytes31\"},{\"internalType\":\"bytes32\",\"name\":\"bytes32Value\",\"type\":\"bytes32\"},{\"internalType\":\"bytes33\",\"name\":\"bytes33Value\",\"type\":\"bytes33\"},{\"internalType\":\"bytes63\",\"name\":\"bytes63Value\",\"type\":\"bytes63\"},{\"internalType\":\"bytes64\",\"name\":\"bytes64Value\",\"type\":\"bytes64\"}],\"internalType\":\"structEventEmitter.BoundaryEdges\",\"name\":\"edges\",\"type\":\"tuple\"}],\"name\":\"echoBoundaryEdges\",\"outputs\":[{\"components\":[{\"internalType\":\"uint248\",\"name\":\"unsigned248\",\"type\":\"uint248\"},{\"internalType\":\"int248\",\"name\":\"signed248\",\"type\":\"int248\"},{\"internalType\":\"uint256\",\"name\":\"unsigned256\",\"type\":\"uint256\"},{\"internalType\":\"int256\",\"name\":\"signed256\",\"type\":\"int256\"},{\"internalType\":\"uint264\",\"name\":\"unsigned264\",\"type\":\"uint264\"},{\"internalType\":\"int264\",\"name\":\"signed264\",\"type\":\"int264\"},{\"internalType\":\"uint504\",\"name\":\"unsigned504\",\"type\":\"uint504\"},{\"internalType\":\"int504\",\"name\":\"signed504\",\"type\":\"int504\"},{\"internalType\":\"uint512\",\"name\":\"unsigned512\",\"type\":\"uint512\"},{\"internalType\":\"int512\",\"name\":\"signed512\",\"type\":\"int512\"},{\"internalType\":\"bytes31\",\"name\":\"bytes31Value\",\"type\":\"bytes31\"},{\"internalType\":\"bytes32\",\"name\":\"bytes32Value\",\"type\":\"bytes32\"},{\"internalType\":\"bytes33\",\"name\":\"bytes33Value\",\"type\":\"bytes33\"},{\"internalType\":\"bytes63\",\"name\":\"bytes63Value\",\"type\":\"bytes63\"},{\"internalType\":\"bytes64\",\"name\":\"bytes64Value\",\"type\":\"bytes64\"}],\"internalType\":\"structEventEmitter.BoundaryEdges\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint16[2][2]\",\"name\":\"fixedMatrix\",\"type\":\"uint16[2][2]\"},{\"internalType\":\"uint16[2][]\",\"name\":\"rows\",\"type\":\"uint16[2][]\"},{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"internalType\":\"uint16[][]\",\"name\":\"values\",\"type\":\"uint16[][]\"}],\"internalType\":\"structEventEmitter.DynamicRecord[2]\",\"name\":\"records\",\"type\":\"tuple[2]\"},{\"components\":[{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"bytes64\",\"name\":\"tag\",\"type\":\"bytes64\"}],\"internalType\":\"structEventEmitter.Record\",\"name\":\"fixedRecord\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"internalType\":\"uint16[][]\",\"name\":\"values\",\"type\":\"uint16[][]\"}],\"internalType\":\"structEventEmitter.DynamicRecord\",\"name\":\"dynamicRecord\",\"type\":\"tuple\"},{\"internalType\":\"bytes\",\"name\":\"extra\",\"type\":\"bytes\"}],\"internalType\":\"structEventEmitter.NestedRecord\",\"name\":\"nested\",\"type\":\"tuple\"}],\"name\":\"echoCompositeContainers\",\"outputs\":[{\"internalType\":\"uint16[2][2]\",\"name\":\"\",\"type\":\"uint16[2][2]\"},{\"internalType\":\"uint16[2][]\",\"name\":\"\",\"type\":\"uint16[2][]\"},{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"internalType\":\"uint16[][]\",\"name\":\"values\",\"type\":\"uint16[][]\"}],\"internalType\":\"structEventEmitter.DynamicRecord[2]\",\"name\":\"\",\"type\":\"tuple[2]\"},{\"components\":[{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"bytes64\",\"name\":\"tag\",\"type\":\"bytes64\"}],\"internalType\":\"structEventEmitter.Record\",\"name\":\"fixedRecord\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"internalType\":\"uint16[][]\",\"name\":\"values\",\"type\":\"uint16[][]\"}],\"internalType\":\"structEventEmitter.DynamicRecord\",\"name\":\"dynamicRecord\",\"type\":\"tuple\"},{\"internalType\":\"bytes\",\"name\":\"extra\",\"type\":\"bytes\"}],\"internalType\":\"structEventEmitter.NestedRecord\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes[2]\",\"name\":\"fixedBytes\",\"type\":\"bytes[2]\"},{\"internalType\":\"bytes[]\",\"name\":\"byteSlices\",\"type\":\"bytes[]\"},{\"internalType\":\"string[]\",\"name\":\"strings\",\"type\":\"string[]\"}],\"name\":\"echoDynamicContainers\",\"outputs\":[{\"internalType\":\"bytes[2]\",\"name\":\"\",\"type\":\"bytes[2]\"},{\"internalType\":\"bytes[]\",\"name\":\"\",\"type\":\"bytes[]\"},{\"internalType\":\"string[]\",\"name\":\"\",\"type\":\"string[]\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"function(uint512)pureexternalreturns(uint512)\",\"name\":\"callback\",\"type\":\"function\"},{\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"internalType\":\"function(uint512)pureexternalreturns(uint512)[2]\",\"name\":\"fixedCallbacks\",\"type\":\"function[2]\"},{\"internalType\":\"function(uint512)pureexternalreturns(uint512)[]\",\"name\":\"callbacks\",\"type\":\"function[]\"},{\"components\":[{\"internalType\":\"function(uint512)pureexternalreturns(uint512)\",\"name\":\"callback\",\"type\":\"function\"},{\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"}],\"internalType\":\"structEventEmitter.FunctionRecord\",\"name\":\"record\",\"type\":\"tuple\"}],\"name\":\"echoFunctions\",\"outputs\":[{\"internalType\":\"function(uint512)pureexternalreturns(uint512)\",\"name\":\"\",\"type\":\"function\"},{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"},{\"internalType\":\"function(uint512)pureexternalreturns(uint512)[2]\",\"name\":\"\",\"type\":\"function[2]\"},{\"internalType\":\"function(uint512)pureexternalreturns(uint512)[]\",\"name\":\"\",\"type\":\"function[]\"},{\"components\":[{\"internalType\":\"function(uint512)pureexternalreturns(uint512)\",\"name\":\"callback\",\"type\":\"function\"},{\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"}],\"internalType\":\"structEventEmitter.FunctionRecord\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address[2]\",\"name\":\"fixedAddresses\",\"type\":\"address[2]\"},{\"internalType\":\"address[]\",\"name\":\"addresses\",\"type\":\"address[]\"},{\"internalType\":\"bytes64[2]\",\"name\":\"fixedTags\",\"type\":\"bytes64[2]\"},{\"internalType\":\"bytes64[]\",\"name\":\"tags\",\"type\":\"bytes64[]\"}],\"name\":\"echoLeafContainers\",\"outputs\":[{\"internalType\":\"address[2]\",\"name\":\"\",\"type\":\"address[2]\"},{\"internalType\":\"address[]\",\"name\":\"\",\"type\":\"address[]\"},{\"internalType\":\"bytes64[2]\",\"name\":\"\",\"type\":\"bytes64[2]\"},{\"internalType\":\"bytes64[]\",\"name\":\"\",\"type\":\"bytes64[]\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"internalType\":\"uint16[][]\",\"name\":\"values\",\"type\":\"uint16[][]\"}],\"internalType\":\"structEventEmitter.DynamicRecord\",\"name\":\"record\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"internalType\":\"uint16[][]\",\"name\":\"values\",\"type\":\"uint16[][]\"}],\"internalType\":\"structEventEmitter.DynamicRecord[]\",\"name\":\"records\",\"type\":\"tuple[]\"},{\"internalType\":\"uint16[][][]\",\"name\":\"cube\",\"type\":\"uint16[][][]\"}],\"name\":\"echoNested\",\"outputs\":[{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"internalType\":\"uint16[][]\",\"name\":\"values\",\"type\":\"uint16[][]\"}],\"internalType\":\"structEventEmitter.DynamicRecord\",\"name\":\"\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"internalType\":\"uint16[][]\",\"name\":\"values\",\"type\":\"uint16[][]\"}],\"internalType\":\"structEventEmitter.DynamicRecord[]\",\"name\":\"\",\"type\":\"tuple[]\"},{\"internalType\":\"uint16[][][]\",\"name\":\"\",\"type\":\"uint16[][][]\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"internalType\":\"uint16[][]\",\"name\":\"values\",\"type\":\"uint16[][]\"}],\"internalType\":\"structEventEmitter.DynamicRecord\",\"name\":\"record\",\"type\":\"tuple\"},{\"internalType\":\"uint16[3]\",\"name\":\"fixedNumbers\",\"type\":\"uint16[3]\"},{\"internalType\":\"string[2]\",\"name\":\"fixedStrings\",\"type\":\"string[2]\"},{\"internalType\":\"uint16[][2]\",\"name\":\"mixed\",\"type\":\"uint16[][2]\"}],\"name\":\"emitComposite\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bool\",\"name\":\"flag\",\"type\":\"bool\"},{\"internalType\":\"bytes5\",\"name\":\"code\",\"type\":\"bytes5\"},{\"internalType\":\"int16\",\"name\":\"delta\",\"type\":\"int16\"}],\"name\":\"emitIndexedScalars\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint16\",\"name\":\"marker\",\"type\":\"uint16\"},{\"internalType\":\"uint512\",\"name\":\"value\",\"type\":\"uint512\"}],\"name\":\"emitPinged\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"bytes64\",\"name\":\"tag\",\"type\":\"bytes64\"}],\"internalType\":\"structEventEmitter.Record\",\"name\":\"record\",\"type\":\"tuple\"}],\"name\":\"emitRecordSeen\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"string\",\"name\":\"value\",\"type\":\"string\"}],\"name\":\"emitTransformed\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint16\",\"name\":\"value\",\"type\":\"uint16\"}],\"name\":\"emitTransformed\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"function(uint512)pureexternalreturns(uint512)\",\"name\":\"callback\",\"type\":\"function\"},{\"internalType\":\"uint512\",\"name\":\"value\",\"type\":\"uint512\"}],\"name\":\"exerciseFunction\",\"outputs\":[{\"internalType\":\"function(uint512)pureexternalreturns(uint512)\",\"name\":\"\",\"type\":\"function\"},{\"internalType\":\"uint512\",\"name\":\"\",\"type\":\"uint512\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint512\",\"name\":\"code\",\"type\":\"uint512\"},{\"internalType\":\"string\",\"name\":\"reason\",\"type\":\"string\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"components\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"bytes64\",\"name\":\"tag\",\"type\":\"bytes64\"}],\"internalType\":\"structEventEmitter.Record\",\"name\":\"record\",\"type\":\"tuple\"},{\"internalType\":\"uint16[][]\",\"name\":\"nested\",\"type\":\"uint16[][]\"}],\"name\":\"failComplex\",\"outputs\":[],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"failHalted\",\"outputs\":[],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"failPanic\",\"outputs\":[],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"failReason\",\"outputs\":[],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"observe\",\"outputs\":[{\"internalType\":\"uint512\",\"name\":\"value\",\"type\":\"uint512\"},{\"internalType\":\"address\",\"name\":\"caller\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint16\",\"name\":\"marker\",\"type\":\"uint16\"}],\"name\":\"pay\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint512\",\"name\":\"value\",\"type\":\"uint512\"}],\"name\":\"plusOne\",\"outputs\":[{\"internalType\":\"uint512\",\"name\":\"\",\"type\":\"uint512\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint512\",\"name\":\"amount\",\"type\":\"uint512\"},{\"internalType\":\"int512\",\"name\":\"delta\",\"type\":\"int512\"},{\"internalType\":\"bytes64\",\"name\":\"tag\",\"type\":\"bytes64\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"payload\",\"type\":\"bytes\"},{\"internalType\":\"string\",\"name\":\"note\",\"type\":\"string\"},{\"internalType\":\"bool\",\"name\":\"enabled\",\"type\":\"bool\"}],\"name\":\"store\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"string\",\"name\":\"value\",\"type\":\"string\"}],\"name\":\"transform\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint16\",\"name\":\"value\",\"type\":\"uint16\"}],\"name\":\"transform\",\"outputs\":[{\"internalType\":\"uint16\",\"name\":\"\",\"type\":\"uint16\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"stateMutability\":\"payable\",\"type\":\"receive\"}]",
	Bin: "0x6101006080526080516134c53803a06134c5a339a1016080a1b052610023b1610248565b5fa5b0556080517e080761fb3eddcdd63161e23554f1566d1bc4623a2a4f3be0b73987716811df6101091bb0610062b0a7b0a7b0a7b0a7b0a7b0610328565b608051a0b103b0c150505050506103bb565b634e487b716101e01b5f52604160045260445ffd5b608051603fa201603f1916a1016001600160401b03a111a2a21017156100b1576100b1610074565b608052b1b050565b5f5ba3a110156100d357a1a10151a3a201526040016100bb565ba35ba1a110156100ea575fa1a501536001016100d5565b5050505050565b5f6001600160401b03a3111561010957610109610074565b61011c603fa401603f1916604001610089565bb050a2a152a3a3a301111561012f575fa0fd5b61013da36040a301a46100b9565bb3b2505050565b5fa2603fa30112610153575fa0fd5b61013da3a3516040a5016100f1565b5f60c0a2a4031215610172575fa0fd5b60805160c0a1016001600160401b03a111a2a210171561019457610194610074565ba060805250a0b150a251a1526040a301516040a201526080a301516080a2015250b2b15050565b5fa2603fa301126101ca575fa0fd5ba15160406001600160401b03a211156101e5576101e5610074565ba160061b6101f4a2a201610089565bb2a352a4a101a201b2a2a101b0a7a5111561020d575fa0fd5ba3a701b2505ba4a3101561023d57a25161ffffa116a0a21461022d575fa0fd5ba35250b1a301b1b0a301b0610213565bb7b650505050505050565b5fa05fa05f6101c0a6a803121561025d575fa0fd5ba5516040a70151b0b5506001600160401b03a0a2111561027b575fa0fd5ba1a801b150a8603fa3011261028e575fa0fd5b61029da9a3516040a5016100f1565bb5506080a80151b150a0a211156102b2575fa0fd5b6102bea9a3aa01610144565bb4506102cda960c0aa01610162565bb350610180a80151b150a0a211156102e3575fa0fd5b506102f0a8a2a9016101bb565bb15050b2b550b2b5b0b350565b5fa151a0a452610314a16040a6016040a6016100b9565b603f01603f1916b2b0b201604001b2b15050565b5f6101c0a7a3526040a1a1a50152610342a2a501a96102fd565bb150a3a2036080a50152610356a2a86102fd565ba65160c0a60152a1a70151610100a601526080a70151610140a60152a4a103610180a60152a551a0a252a6a301b350b0a201b05f5ba1a110156103ab57a45161ffff16a352b3a301b3b1a301b160010161038b565b50b0bab950505050505050505050565b6130fda06103c85f395ff3fe6101006080526004361061012b575f356101e01ca0630753c06a146101a457a06314fc78fc146101dc57a0631e3ed7e4146101fd57a0632f6077c01461021e57a0632fb0dbcd1461023257a0633b0e4d671461024557a0633d0e10891461027857a0634b79d0e31461029757a0634dc96ec0146102c957a06350aa10c9146102f557a063572a647e1461032357a06379531c401461034257a06399cf235f1461036f57a0639e420a8f1461038e57a063a43e73c9146103ba57a063af73dcd1146103e857a063b0b754361461041757a063b2c83e2c1461042b57a063b94d6fa61461044a57a063c66c90281461046957a063e558a3a71461047d57a063ed928c96146104ae57a063f404ae99146104e057a063f8041229146104ff57a063fb1447221461052d5761016d565b3661016d577fa8142743f8f70a4c26f3691cf4ed59718381fb2f18070ec52be1f1022d8555576101001b34608051610163b1b06111c3565b608051a0b103b0c1005b7fe5b92b8ba08394dd9b027fafca0dc888f149e8f420b55893ecee14ea148aa08b6101001b5f3634608051610163b3b2b1b0611200565b34a0156101af575fa0fd5b506101c36101be366004611283565b61054c565b6080516101d3b4b3b2b1b0611376565b608051a0b103b0f35b34a0156101e7575fa0fd5b505f546080a051b1a252336040a30152016101d3565b34a015610208575fa0fd5b5061021c610217366004611447565b6106ae565b005b34a015610229575fa0fd5b5061021c6106ef565b61021c61024036600461149b565b610709565b34a015610250575fa0fd5b5061026461025f36600461151b565b61074d565b6080516101d3b8b7b6b5b4b3b2b1b06116e0565b34a015610283575fa0fd5b5061021c610292366004611796565b6107f1565b34a0156102a2575fa0fd5b506102b66102b1366004611796565b6108b8565b6080516101d3b7b6b5b4b3b2b1b0611837565b34a0156102d4575fa0fd5b506102e86102e3366004611890565b610985565b6080516101d3b1b06118a7565b34a015610300575fa0fd5b5061031461030f3660046119bd565b610a16565b6080516101d3b3b2b1b0611aa2565b34a01561032e575fa0fd5b5061021c61033d366004611b6a565b610a5e565b34a01561034d575fa0fd5b5061036161035c366004611b84565b610ab4565b608051b0a1526040016101d3565b34a01561037a575fa0fd5b5061021c610389366004611bac565b610ac0565b34a015610399575fa0fd5b506103ad6103a8366004611447565b610b07565b6080516101d3b1b0611c42565b34a0156103c5575fa0fd5b506103d96103d4366004611c54565b610b50565b6080516101d3b3b2b1b0611c93565b34a0156103f3575fa0fd5b50610407610402366004611d01565b610c3c565b6080516101d3b4b3b2b1b0611f32565b34a015610422575fa0fd5b5061021c610d96565b34a015610436575fa0fd5b5061021c610445366004611fcf565b610de1565b34a015610455575fa0fd5b5061021c610464366004611ff7565b610df5565b34a015610474575fa0fd5b5061021c610e3d565b34a015610488575fa0fd5b5061049c610497366004612040565b610e47565b6080516101d3b6b5b4b3b2b1b0612138565b34a0156104b9575fa0fd5b506104cd6104c836600461149b565b610fa1565b60805161ffffb0b116a1526040016101d3565b34a0156104eb575fa0fd5b5061021c6104fa36600461149b565b610fad565b34a01561050a575fa0fd5b5061051e6105193660046121f1565b610feb565b6080516101d3b3b2b1b0612227565b34a015610538575fa0fd5b5061021c6105473660046122ec565b61101f565b610554611049565b60c061055e611049565b60c0a9a9a9a9a9a9a56002a0604002608051b0a101608052a0b2b1b0a260025fb25ba1a4101561059e57a235a152604001b1604001b1b2600101b2610580565bb2505050505050b550a4a4a0a0604002604001608051b0a101608052a0b3b2b1b0a1600160016101001b0316a152604001a3a35fb25ba1a410156105f257a235a152604001b1604001b1b2600101b26105d4565bb250505050505050b350b0b1b2b350a26002a0604002608051b0a101608052a0b2b1b0a260025fb25ba1a4101561063957a235a152604001b1604001b1b2600101b261061b565bb2505050505050b250a1a1a0a0604002604001608051b0a101608052a0b3b2b1b0a1600160016101001b0316a152604001a3a35fb25ba1a4101561068d57a235a152604001b1604001b1b2600101b261066f565bb250505050505050b050b050b350b350b350b350b650b650b650b6b2505050565b7f29d8416f597bcc46fa3c441ff72963f4a2852e9c6d77447615f782a1ca0da3576101011ba2a26080516106e3b2b1b061239d565b608051a0b103b0c15050565b608051631ee9080f6101e01ba152600401608051a0b103b0fd5ba061ffff16337f1398d89bb96c43f8c16ef74dee904b456a4fa8a5857191293b848ced1997a3d96101001b34608051610742b1b06111c3565b608051a0b103b0c350565b5fa05fa05f61075a611067565b610762611085565b61076a611085565bafafafafafafafafa26003a0604002608051b0a101608052a0b2b1b0a260035fb25ba1a410156107ae57a23561ffff16a152604001b1604001b1b2600101b261078c565bb2505050505050b250a16107c1b06124cd565bb1506107cca16125c4565bb050b750b750b750b750b750b750b750b750b850b850b850b850b850b850b850b8b050565ba85fa1b05550a7a9a77f0971a927eb69632cd5aced366c9dd3ee5626b6c0a27cb781139eeffab9e5372f6101001baaa9a9a9a9a9608051610837b6b5b4b3b2b1b0612618565b608051a0b103b0c4a2a260805161084fb2b1b061265d565b608051a0b103b0206101001ba5a560805161086bb2b1b061265d565b608051b0a1b003a120aba2526101001bb07f4ef7447df163d4aaeab9c66fa93651de5eebb002dcf9b60da1ebaa28ae95e8256101001bb0604001608051a0b103b0c3505050505050505050565b5fa05fa060c0a05fafafafafafafafafafa4a4a0a0603f016040a0b10402604001608051b0a101608052a0b3b2b1b0a1600160016101001b0316a152604001a3a3a0a2a4375fb201b1b0b15250506080a0516040603fa801a1b004a102a201a101b0b252600160016101001b03a716a152b3b850b5b650b3b4b2b350b0b1a5b150a4b0a1b0a401a3a2a0a2a4375fa1a40152603f19603fa20116b050a0a301b250505050505050b150b0b150b650b650b650b650b650b650b650b950b950b950b950b950b950b9b2505050565b6080a0516103c0a101a2525fa0a2526040a201a1b052b1a101a2b05260c0a101a2b052610100a101a2b052610140a101a2b052610180a101a2b0526101c0a101a2b052610200a101a2b052610240a101a2b052610280a101a2b0526102c0a101a2b052610300a101a2b052610340a101a2b052610380a101b1b0b152610a1036a3b003a301a361273b565bb2b15050565b610a1e611085565b60c0a0a7a7a7a7a7610a2fa56128e7565bb450610a3ba3a561293b565bb350b0b150610a4aa1a36129aa565bb3bcb2bb50b2b950b0b75050505050505050565b6080a051a235a1526040a0a40135b0a20152a2a20135a1a30152b051b0a1b00360c001a1206101001bb07f2c56d834ffca88819ed7b3502263457d7af7418782a1d711a6dba651107f26096101011bb05fb0c250565b5f610a10a26001612a22565b7f52ebad060d7f3dc17d6ea0e956b35cfd849a6a551b539872ed459157021a97076101011ba4a4a4a4608051610af9b4b3b2b1b0612c14565b608051a0b103b0c150505050565b60c0a2a2a0a0603f016040a0b10402604001608051b0a101608052a0b3b2b1b0a1600160016101001b0316a152604001a3a3a0a2a4375fb201b1b0b15250b2b6b5505050505050565b5fa05fa0a6a6a6608051a263ffffffff166101e01ba152600401610b76b1a152604001b0565b6040608051a0a303a1a65afa15a015610b91573d5fa03e3d5ffd5b505050506080513d603f01603f19163da1a11015610bb257a0a20336a2a501375b50a1016080a1b052610bc3b1612cfb565b608051a8a152600160016101e01b03196101e0a9b01b166040a20152b0b150604401608051a0b103b0206101001b7f3e85e019f156fb371415540e280c0864415370980dac796b574f42a76aa4d08f6101021ba8a8a4608051610c28b3b2b1b0611c93565b608051a0b103b0c2b5b6b4b5b4b350505050565b610c446110ac565b60c0610c4e6110e3565b610c5661111a565ba8a8a8a8a8a46002a0604002608051b0a101608052a0b2b1b05fb05ba2a21015610cd1576080a051a0a201a252b0a302a50160025fa2a2a55ba1a41015610cb157a23561ffff16a152604001b1604001b1b2600101b2610c8f565bb2505050505050600160016101001b0316a152604001b0600101b0610c72565b50505050b450a3a3a0a0604002604001608051b0a101608052a0b3b2b1b0a1600160016101001b0316a1526040015fb05ba2a21015610d61576080a051a0a201a252b0a302a60160025fa2a2a55ba1a41015610d4157a23561ffff16a152604001b1604001b1b2600101b2610d1f565bb2505050505050600160016101001b0316a152604001b0600101b0610d02565b5050505050b250b0b1b250a1610d76b0612e41565bb150610d81a1612e95565bb3bdb2bc50b0ba50b1b850b650505050505050565b60805162461bcd6101e51ba15260406004a2015260196044a20152782b269039ba30b73230b932103932bb32b93a103932b0b9b7b76101391b6084a2015260c4015b608051a0b103b0fd5b608051a1a15261ffffa316b06040016106e3565ba060010ba2600160016101d81b031916a415157f19c59af463d0b89e6afb02db53c6ea998a04ce7bf1aa5c2c0d4c3ac9efc9e6596101001b608051608051a0b103b0c4505050565b610e45612f37565b565b5fa060c0610e5361116e565b6080a05160c0a1a101a3525f6040a301a1b052a252b1a101a2b052adadadadadadadada5a5a0a0603f016040a0b10402604001608051b0a101608052a0b3b2b1b0a1600160016101001b0316a152604001a3a3a0a2a4375fb201a2b052506080a051610100a101b0b152b4ba50b7b850b5b6b4b550b2b3b1b250a6b16002b150a2a2a55ba1a41015610f06576040a3a1013563ffffffff16b0a20152a235a1526001b3b0b301b26080b2a301b201610ed7565bb2505050505050b350a2a2a0a0608002604001608051b0a101608052a0b3b2b1b0a1600160016101001b0316a152604001a3a35fb25ba1a41015610f6b576040a3a1013563ffffffff16b0a20152a235a1526001b3b0b301b26080b2a301b201610f3c565bb250505050505050b150b0b150a0610f82b0612f4c565bb050b550b550b550b550b550b550b850b850b850b850b850b8b2505050565b5f610a10a26001612fa8565b60805161ffffa216a1527f38e10c946553786c68c20c6706a95e949fdaa40be4cdb80e325a15f92c2e08d56101021bb0604001608051a0b103b0c150565b610ff361119c565b60c0a0a7a7a7a7a7611004a5612fca565bb450611010a3a5612fd5565bb350b0b150610a4aa1a3613038565ba7a7a7a7a7a7a7a760805163a6b3cee16101e01ba152600401610dd8b8b7b6b5b4b3b2b1b061309b565b608051a0608001608052a06002b06040a202a036a33750b1b2b15050565b608051a060c001608052a06003b06040a202a036a33750b1b2b15050565b608051a0608001608052a06002b05b60c0a1525f19b0b101b0604001a161109457b05050b0565b608051a0608001608052a06002b05b6110c3611049565b600160016101001b0316a152604001b06001b003b0a16110bb57b05050b0565b608051a0608001608052a06002b05b6110fa61119c565b600160016101001b0316a152604001b06001b003b0a16110f257b05050b0565b6080a051610180a101b0b1525f60c0a201a1a152610100a301a2b052610140a301b1b0b152600160016101001b0316a1526040a10161115761119c565b600160016101001b0316a15260c06040b0b10152b0565b608051a061010001608052a06002b05b5f6040a201a1b052a1525f19b0b101b0608001a161117e57b05050b0565b6080a051610100a101a2525fa15260c06040a201a1b052b1a101a2b052a1a101b1b0b152b0565b600160016101001b03b1b0b116a152604001b0565ba1a352a1a16040a50137505fa2a2016040b0a101b1b0b152603fb0b101603f1916b0b10101b0565b6080a1525f6112136080a301a5a76111d8565bb0506001a06101001b03a3166040a30152b4b350505050565ba06080a101a31015610a10575fa0fd5b5fa0a3603fa4011261124c575fa0fd5b50a1356001600160401b03a11115611262575fa0fd5b6040a301b150a36040a260061ba50101111561127c575fa0fd5bb250b2b050565b5fa05fa05fa0610180a7a9031215611299575fa0fd5b6112a3a8a861122c565bb5506080a701356001600160401b03a0a211156112be575fa0fd5b6112caaaa3ab0161123c565bb0b750b550a5b1506112dfaa60c0ab0161122c565bb450610140a90135b150a0a211156112f5575fa0fd5b50611302a9a2aa0161123c565bb7bab6b950b4b750b2b5b3b4b2505050565ba05f5b6002a1101561133657a151a4526040b3a401b3b0b101b0600101611317565b50505050565b5fa151a0a4526040a0a501b4506040a4015f5ba3a1101561136b57a151a752b5a201b5b0a201b060010161134f565b50b4b5b45050505050565b5f610180a2a101a3a8a45b6002a110156113a057a151a3526040b2a301b2b0b101b0600101611381565b5050506080a401b1b0b152a551b0a1b0526101c0a301b06040b0a1a8015f5ba2a110156113db57a151a552b3a301b3b0a301b06001016113bf565b505050506113ec60c0a401a6611314565ba2a103610140a401526113ffa1a561133c565bb7b650505050505050565b5fa0a3603fa4011261141a575fa0fd5b50a1356001600160401b03a11115611430575fa0fd5b6040a301b150a36040a2a50101111561127c575fa0fd5b5fa06040a3a5031215611458575fa0fd5ba2356001600160401b03a1111561146d575fa0fd5b611479a5a2a60161140a565bb0b6b0b550b350505050565ba03561ffffa116a114611496575fa0fd5bb1b050565b5f6040a2a40312156114ab575fa0fd5b6114b4a2611485565bb3b2505050565ba0355fa1b00ba114611496575fa0fd5ba035600160016101001b03a116a114611496575fa0fd5ba035601fa1b00ba114611496575fa0fd5ba035600160016101d81b0319a116a114611496575fa0fd5ba060c0a101a31015610a10575fa0fd5b5fa05fa05fa05fa0610280a9ab031215611533575fa0fd5ba83560ffa116a114611543575fa0fd5bb7506115516040aa016114bb565bb65061155f6080aa016114cb565bb55061156d60c0aa016114e2565bb45061157c610100aa016114f3565bb35061158caa610140ab0161150b565bb250610200a901356001600160401b03a0a211156115a8575fa0fd5b6115b4aca3ad0161122c565bb350610240ab0135b150a0a211156115ca575fa0fd5b506115d7aba2ac0161122c565bb15050b2b5b850b2b5b8b0b3b650565b5fa151a0a4525f5ba1a1101561160b576040a1a501a10151a6a301a20152016115ef565ba15ba1a11015611625575f6040a2a801015360010161160d565b5050603f01603f1916b2b0b201604001b2b15050565b5fa26080a101a35f5b6002a1101561167357a3a303a75261165da3a3516115e7565b6040b7a801b7b0b350b1b0b101b0600101611644565b50b0b5b45050505050565b5fa26080a101a35f5b6002a1101561167357a3a303a752a151a051a0a5526040b1a201b1a0a601b1b05f5ba2a110156116c957a45161ffff16a452b3a101b3b2a101b26001016116a9565b50b9aa01b9b1b55050b2b0b201b150600101611687565b60ffa916a1525fa8a10b6040a0a401b1b0b152600160016101001b03a9166080a40152601fa8b00b60c0a40152600160016101d81b0319a716610100a40152610280b0610140a401a7a45b6003a1101561174c57a15161ffff16a352b1a301b1b0a301b060010161172b565b50505050a0610200a40152611763a1a401a661163b565bb050a2a103610240a40152611778a1a561167e565bbbba5050505050505050505050565ba035a01515a114611496575fa0fd5b5fa05fa05fa05fa05f6101c0aaac0312156117af575fa0fd5ba935b8506040aa0135b7506080aa0135b65060c0aa0135b550610100aa01356001600160401b03a0a211156117e2575fa0fd5b6117eeada3ae0161140a565bb0b750b550610140ac0135b150a0a21115611807575fa0fd5b50611814aca2ad0161140a565bb0b450b250611828b050610180ab01611787565bb050b2b5b850b2b5b850b2b5b8565b5f6101c0a9a352a86040a40152a76080a40152a660c0a40152a0610100a40152611863a1a401a76115e7565bb050a2a103610140a40152611878a1a66115e7565bb15050a21515610180a30152b8b75050505050505050565b5f6103c0a2a40312156118a1575fa0fd5b50b1b050565ba1516001600160f81b0316a1526103c0a1016040a301516118cd6040a401a2601e0bb052565b506080a301516118e96080a401a2600160016101001b0316b052565b5060c0a301516118fe60c0a401a2601f0bb052565b50610100a3a10151600160016101081b0316b0a30152610140a0a4015160200bb0a30152610180a0a40151600160016101f81b0316b0a301526101c0a0a40151603e0bb0a30152610200a0a40151b0a30152610240a0a40151b0a30152610280a0a40151600160016101081b031916b0a301526102c0a0a40151600160016101001b031916b0a30152610300a0a401516001600160f81b031916b0a30152610340a0a4015160ff1916b0a30152610380b2a30151b2b0b101b1b0b152b0565b5fa05fa05f60c0a6a80312156119d1575fa0fd5ba5356001600160401b03a0a211156119e7575fa0fd5b6119f3a9a3aa0161122c565bb6506040a80135b150a0a21115611a08575fa0fd5b611a14a9a3aa0161123c565bb0b650b4506080a80135b150a0a21115611a2c575fa0fd5b50611a39a8a2a90161123c565bb6b9b5b850b3b650b2b4b3b2505050565b5fa2a251a0a5526040a0a601b5506040a260061ba401016040a6015f5ba4a11015611a9557603f19a6a40301a952611a83a3a3516115e7565bb8a401b8b250b0a301b0600101611a67565b50b0b7b650505050505050565b60c0a0a2525fb0610140a301b0a301a6a35b6002a11015611ae65760bf19a6a50301a352611ad1a4a3516115e7565bb3506040b2a301b2b1b0b101b0600101611ab4565b5050506040a3a203a1a50152a1a651a0a452a2a401b150a2a160061ba50101a3a9015f5ba3a11015611b3857603f19a7a40301a552611b26a3a3516115e7565bb4a601b4b250b0a501b0600101611b0a565b5050a6a1036080a80152611b4ca1a9611a4a565bbab950505050505050505050565b5f60c0a2a40312156118a1575fa0fd5b5f60c0a2a4031215611b7a575fa0fd5b6114b4a3a3611b5a565b5f6040a2a4031215611b94575fa0fd5b5035b1b050565b5f610100a2a40312156118a1575fa0fd5b5fa05fa0610180a5a7031215611bc0575fa0fd5ba4356001600160401b03a0a21115611bd6575fa0fd5b611be2a8a3a901611b9b565bb550611bf1a86040a90161150b565bb450610100a70135b150a0a21115611c07575fa0fd5b611c13a8a3a90161122c565bb350610140a70135b150a0a21115611c29575fa0fd5b50611c36a7a2a80161122c565bb15050b2b5b1b450b250565b6040a1525f6114b46040a301a46115e7565b5fa05f60c0a4a6031215611c66575fa0fd5b5050a135b36040a3013563ffffffff16b3506080b0b20135b1b050565ba25263ffffffff166040b0b10152565b60c0a101611ca2a2a5a7611c83565ba26080a30152b4b350505050565ba0610100a101a31015610a10575fa0fd5b5fa0a3603fa40112611cd1575fa0fd5b50a1356001600160401b03a11115611ce7575fa0fd5b6040a301b150a36040a260071ba50101111561127c575fa0fd5b5fa05fa05f6101c0a6a8031215611d16575fa0fd5b611d20a7a7611cb0565bb450610100a601356001600160401b03a0a21115611d3c575fa0fd5b611d48a9a3aa01611cc1565bb0b650b450610140b150a7a20135a1a11115611d62575fa0fd5b611d6eaaa2ab0161122c565bb45050610180a80135a1a11115611d83575fa0fd5ba801b050a0a903a21315611d95575fa0fd5ba0b2505050b2b550b2b5b0b350565b5fa2a2a25b6002a11015611dcc57a15161ffff16a3526040b2a301b2b0b101b0600101611da9565b5050506080a301b050b2b15050565b5fa2a251a0a5526040a0a601b550a0a260061ba40101a1a6015f5ba4a11015611a9557a5a303603f1901a952a151a051a0a552b0a501b0a5a501b05f5ba1a11015611e3857a35161ffff16a352b2a701b2b1a701b1600101611e18565b5050b9a501b9b35050b0a301b0600101611df6565b5f610100a251a4526040a30151a16040a60152611e6ca2a601a26115e7565bb150506080a30151a4a2036080a60152611e86a2a26115e7565bb1505060c0a30151a4a20360c0a60152611ea0a2a2611ddb565bb5b45050505050565b5fa26080a101a35f5b6002a1101561167357a3a303a752611ecba3a351611e4d565b6040b7a801b7b0b350b1b0b101b0600101611eb2565b5f610140a251a051a5526040a101516040a601526080a101516080a60152506040a30151a160c0a60152611f17a2a601a2611e4d565bb150506080a30151a4a203610100a60152611ea0a2a26115e7565b5f6101c0a2a101a3a8a45b6002a11015611f6257611f51a3a351611da4565bb2506040b1b0b101b0600101611f3d565b505050610100a401b1b0b152a551b0a1b052610200a301b06040b0a1a8015f5ba2a11015611fa357611f95a5a351611da4565bb450b0a301b0600101611f82565b50505050a2a103610140a40152611fbaa1a6611ea9565bb050a2a103610180a401526113ffa1a5611ee1565b5fa06080a3a5031215611fe0575fa0fd5b611fe9a3611485565bb46040b3b0b30135b3505050565b5fa05f60c0a4a6031215612009575fa0fd5b612012a4611787565bb2506120206040a5016114f3565bb1506080a40135a060010ba114612035575fa0fd5ba0b15050b250b250b2565b5fa05fa05fa05fa0610240a9ab031215612058575fa0fd5ba835b7506040a9013563ffffffff16b6506080a901356001600160401b03a0a21115612082575fa0fd5b61208eaca3ad0161140a565bb0b850b650a6b1506120a3ac60c0ad01611cb0565bb5506101c0ab0135b150a0a211156120b9575fa0fd5b6120c5aca3ad01611cc1565bb0b550b350610200ab0135b150a0a211156120de575fa0fd5b506115d7aba2ac01611b5a565ba0516040b0b10151b0b163ffffffffb0b116b0565b5f61210aa26120eb565b612115a5a2a4611c83565b50506080a2015160c06080a5015261213060c0a501a26115e7565bb4b350505050565b5f610240612147a3a9ab611c83565b6080a16080a5015261215ba2a501a96115e7565bb15060c0a401a75f5b6002a1101561219357612176a26120eb565b612181a5a2a4611c83565b5050b1a301b1b0a301b0600101612164565b505050a3a2036101c0a50152a551a0a3526040a0a801b301b05f5ba1a110156121dc576121bfa56120eb565b6121caa5a2a4611c83565b5050b3a301b3b1a301b16001016121ae565b5050a4a103610200a60152611778a1a7612100565b5fa05fa05f60c0a6a8031215612205575fa0fd5ba5356001600160401b03a0a2111561221b575fa0fd5b6119f3a9a3aa01611b9b565b60c0a1525f61223960c0a301a6611e4d565b6040a3a203a1a50152a1a651a0a452a2a401b150a2a160061ba50101a3a9015f5ba3a1101561228857603f19a7a40301a552612276a3a351611e4d565bb4a601b4b250b0a501b060010161225a565b5050a6a1036080a80152a751a0a252a4a201b550b2506006a3b01ba101a401b150a3a8015f5ba4a110156122dc57603f19a3a50301a7526122caa4a351611ddb565bb6a601b6b350b0a501b06001016122ae565b50b1bab950505050505050505050565b5fa05fa05fa05fa06101c0a9ab031215612304575fa0fd5ba835b7506040a901356001600160401b03a0a21115612321575fa0fd5b61232daca3ad0161140a565bb0b950b7506080ab0135b150a0a21115612345575fa0fd5b612351aca3ad0161140a565bb0b750b550a5b150612366ac60c0ad01611b5a565bb450610180ab0135b150a0a2111561237c575fa0fd5b50612389aba2ac0161123c565bb9bcb8bb50b6b950b4b7b3b6b2b5b4505050565b6040a1525f6121306040a301a4a66111d8565b634e487b716101e01b5f52604160045260445ffd5b6080516103c0a1016001600160401b03a111a2a21017156123e8576123e86123b0565b608052b0565b60805160c0a1016001600160401b03a111a2a21017156123e8576123e86123b0565b6080a051b0a1016001600160401b03a111a2a21017156123e8576123e86123b0565b608051603fa201603f1916a1016001600160401b03a111a2a210171561245a5761245a6123b0565b608052b1b050565b5fa2603fa30112612471575fa0fd5ba1356001600160401b03a1111561248a5761248a6123b0565b61249d603fa201603f1916604001612432565ba1a152a46040a3a6010111156124b1575fa0fd5ba16040a5016040a301375fb1a101604001b1b0b152b3b2505050565b5f6124d6612410565ba06080a40136a111156124e7575fa0fd5ba45ba1a1101561252157a0356001600160401b03a11115612506575fa0fd5b61251236a2a901612462565ba552506040b3a401b3016124e9565b50b0b4b350505050565b5f6001600160401b03a21115612543576125436123b0565b5060061b604001b0565b5fa2603fa3011261255c575fa0fd5ba135604061257161256ca361252b565b612432565ba0a3a2526040a201b1506040a460061ba70101b350a6a41115612592575fa0fd5b6040a6015ba4a110156125b95761ffff6125aba2611485565b16a352b1a301b1a301612597565b50b6b5505050505050565b5f6125cd612410565ba06080a40136a111156125de575fa0fd5ba45ba1a1101561252157a0356001600160401b03a111156125fd575fa0fd5b61260936a2a90161254d565ba552506040b3a401b3016125e0565b5f610100a8a352a06040a40152612632a1a401a8aa6111d8565bb050a2a1036080a40152612647a1a6a86111d8565bb15050a2151560c0a30152b7b650505050505050565ba1a3a2375fb101b0a152b1b050565ba0356001600160f81b03a116a114611496575fa0fd5ba035601ea1b00ba114611496575fa0fd5ba035600160016101081b03a116a114611496575fa0fd5ba0356020a1b00ba114611496575fa0fd5ba035600160016101f81b03a116a114611496575fa0fd5ba035603ea1b00ba114611496575fa0fd5ba035600160016101081b0319a116a114611496575fa0fd5ba035600160016101001b0319a116a114611496575fa0fd5ba0356001600160f81b0319a116a114611496575fa0fd5ba03560ff19a116a114611496575fa0fd5b5f6103c0a2a403121561274c575fa0fd5b6127546123c5565b61276d612760a461266c565b6001600160f81b0316a252565b61278661277c6040a501612682565b601e0b6040a30152565b6127a66127956080a5016114cb565b600160016101001b03166080a30152565b6127bf6127b560c0a5016114e2565b601f0b60c0a30152565b6101006127e06127d0a2a601612693565b600160016101081b0316a3a30152565b506101406127fb6127f2a2a6016126aa565b60200ba3a30152565b5061018061281d61280da2a6016126bb565b600160016101f81b0316a3a30152565b506101c061283861282fa2a6016126d2565b603e0ba3a30152565b50610200a3a10135b0a20152610240a0a40135b0a20152610280612871612860a2a6016126e3565b600160016101081b031916a3a30152565b506102c0612894612883a2a6016126fb565b600160016101001b031916a3a30152565b506103006128b66128a6a2a601612713565b6001600160f81b031916a3a30152565b506103406128d26128c8a2a60161272a565b60ff1916a3a30152565b50610380b2a30135b2a101b2b0b25250b1b050565b5f6128f0612410565ba06080a40136a11115612901575fa0fd5ba45ba1a1101561252157a0356001600160401b03a11115612920575fa0fd5b61292c36a2a901612462565ba552506040b3a401b301612903565b5f61294861256ca461252b565ba0a4a2526040a0a301b250a560061ba50136a11115612965575fa0fd5ba55ba1a1101561299e57a0356001600160401b03a11115612984575fa0fd5b61299036a2aa01612462565ba65250b3a201b3a201612967565b50b1b6b5505050505050565b5f6129b761256ca461252b565ba0a4a2526040a0a301b250a560061ba50136a111156129d4575fa0fd5ba55ba1a1101561299e57a0356001600160401b03a111156129f3575fa0fd5b6129ff36a2aa01612462565ba65250b3a201b3a2016129d6565b634e487b716101e01b5f52601160045260445ffd5ba0a201a0a21115610a1057610a10612a0d565b5fa0a3356001600160401b03a0a21115612a4d575fa0fd5ba4a201b150603f193601a2113660401117a5a3101715612a6b575fa0fd5ba135b2506040a201b350a0a31115612a81575fa0fd5b50a101604001a2a11036a2111715612a97575fa0fd5b50b250b2b050565b5fa0a3356001600160401b03a0a21115612ab7575fa0fd5ba4a201b150603f193601a2113660401117a5a3101715612ad5575fa0fd5ba135b2506040a201b350a0a31115612aeb575fa0fd5b506006a2b01b01604001a2a11036a2111715612a97575fa0fd5ba1a3525f6040a0a501b450a25f5ba5a1101561136b5761ffff612b27a3611485565b16a752b5a201b5b0a201b0600101612b13565b5fa3a3a5526040a0a601b5506040a560061ba30101a45f5ba7a11015611a9557a4a303603f1901a952612b6da2a8612a9f565b612b78a5a2a4612b05565bbaa601bab4505050b0a301b0600101612b52565b5fa26080a101a35f5b6002a1101561167357a3a303a752612bada2a7612a35565b612bb8a5a2a46111d8565b6040b9aa01b9b0b550b3b0b301b25050600101612b95565b5fa26080a101a35f5b6002a1101561167357a3a303a752612bf1a2a7612a9f565b612bfca5a2a4612b05565b6040b9aa01b9b0b550b3b0b301b25050600101612bd9565b610180a0a252a535b0a201525f6040612c2fa1a801a8612a35565b610100a06101c0a70152612c48610280a701a3a56111d8565bb250612c576080ab01ab612a35565bb25061017f19a0a8a60301610200a90152612c73a5a5a46111d8565bb450612c8260c0ad01ad612a9f565bb450b150a0a8a60301610240a9015250612c9da4a4a3612b3a565bb350506040a601b150a85f5b6003a11015612cd15761ffff612cbea3611485565b16a452b2a501b2b0a501b0600101612ca9565b5050a5a303b0a6015250612ce5a1a7612b8c565bb15050a2a103610140a401526113ffa1a5612bd0565b5f6040a2a4031215612d0b575fa0fd5b5051b1b050565b5fa2603fa30112612d21575fa0fd5ba1356040612d3161256ca361252b565ba2a1526006b2b0b21ba401a101b1a1a101b0a6a41115612d4f575fa0fd5ba2a6015ba4a110156125b957a0356001600160401b03a11115612d70575fa0fd5b612d7ea9a6a3ab010161254d565ba45250b1a301b1a301612d53565b5f610100a0a3a5031215612d9e575fa0fd5b608051b0a101b06001600160401b03a0a311a2a4101715612dc157612dc16123b0565ba2608052a1b350a435a2526040a50135b250a0a31115612ddf575fa0fd5b612deba6a4a701612462565b6040a301526080a50135b250a0a31115612e03575fa0fd5b612e0fa6a4a701612462565b6080a3015260c0a50135b250a0a31115612e27575fa0fd5b50612e34a5a3a601612d12565b60c0a201525050b2b15050565b5f612e4a612410565ba06080a40136a11115612e5b575fa0fd5ba45ba1a1101561252157a0356001600160401b03a11115612e7a575fa0fd5b612e8636a2a901612d8c565ba552506040b3a401b301612e5d565b5fa13603610140a11215612ea7575fa0fd5b612eaf6123ee565b60c0a21215612ebc575fa0fd5b612ec46123ee565ba435a1526040a0a60135b0a201526080a0a60135b0a20152a15260c0a40135b1506001600160401b03a0a31115612ef9575fa0fd5b612f0536a4a701612d8c565b6040a30152610100a50135b250a0a31115612f1e575fa0fd5b50612f2b36a3a601612462565b6080a20152b3b2505050565b634e487b716101e01b5f52600160045260445ffd5b5f60c0a236031215612f5c575fa0fd5b612f646123ee565ba235a1526040a0a4013563ffffffff16b0a201526080a301356001600160401b03a11115612f90575fa0fd5b612f9c36a2a601612462565b6080a3015250b2b15050565b61ffffa1a116a3a21601b0a0a21115612fc357612fc3612a0d565b50b2b15050565b5f610a1036a3612d8c565b5f612fe261256ca461252b565ba0a4a2526040a0a301b250a560061ba50136a11115612fff575fa0fd5ba55ba1a1101561299e57a0356001600160401b03a1111561301e575fa0fd5b61302a36a2aa01612d8c565ba65250b3a201b3a201613001565b5f61304561256ca461252b565ba0a4a2526040a0a301b250a560061ba50136a11115613062575fa0fd5ba55ba1a1101561299e57a0356001600160401b03a11115613081575fa0fd5b61308d36a2aa01612d12565ba65250b3a201b3a201613064565b5f6101c0aaa352a06040a401526130b5a1a401aaac6111d8565bb050a2a1036080a401526130caa1a8aa6111d8565bb050a53560c0a401526040a60135610100a401526080a60135610140a40152a2a103610180a40152611778a1a5a7612b3a56",
}

// EventEmitterABI is the input ABI used to generate the binding from.
// Deprecated: Use EventEmitterMetaData.ABI instead.
var EventEmitterABI = EventEmitterMetaData.ABI

// EventEmitterBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use EventEmitterMetaData.Bin instead.
var EventEmitterBin = EventEmitterMetaData.Bin

// DeployEventEmitter deploys a new QRL contract, binding an instance of EventEmitter to it.
func DeployEventEmitter(auth *bind.TransactOpts, backend bind.ContractBackend, initial *big.Int, note string, payload []byte, record EventEmitterRecord, numbers []uint16) (common.Address, *types.Transaction, *EventEmitter, error) {
	parsed, err := EventEmitterMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(EventEmitterBin), backend, initial, note, payload, record, numbers)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &EventEmitter{EventEmitterCaller: EventEmitterCaller{contract: contract}, EventEmitterTransactor: EventEmitterTransactor{contract: contract}, EventEmitterFilterer: EventEmitterFilterer{contract: contract}}, nil
}

// EventEmitter is an auto generated Go binding around a QRL contract.
type EventEmitter struct {
	EventEmitterCaller     // Read-only binding to the contract
	EventEmitterTransactor // Write-only binding to the contract
	EventEmitterFilterer   // Log filterer for contract events
}

// EventEmitterCaller is an auto generated read-only Go binding around a QRL contract.
type EventEmitterCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EventEmitterTransactor is an auto generated write-only Go binding around a QRL contract.
type EventEmitterTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EventEmitterFilterer is an auto generated log filtering Go binding around a QRL contract events.
type EventEmitterFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EventEmitterSession is an auto generated Go binding around a QRL contract,
// with pre-set call and transact options.
type EventEmitterSession struct {
	Contract     *EventEmitter     // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// EventEmitterCallerSession is an auto generated read-only Go binding around a QRL contract,
// with pre-set call options.
type EventEmitterCallerSession struct {
	Contract *EventEmitterCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts       // Call options to use throughout this session
}

// EventEmitterTransactorSession is an auto generated write-only Go binding around a QRL contract,
// with pre-set transact options.
type EventEmitterTransactorSession struct {
	Contract     *EventEmitterTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts       // Transaction auth options to use throughout this session
}

// EventEmitterRaw is an auto generated low-level Go binding around a QRL contract.
type EventEmitterRaw struct {
	Contract *EventEmitter // Generic contract binding to access the raw methods on
}

// EventEmitterCallerRaw is an auto generated low-level read-only Go binding around a QRL contract.
type EventEmitterCallerRaw struct {
	Contract *EventEmitterCaller // Generic read-only contract binding to access the raw methods on
}

// EventEmitterTransactorRaw is an auto generated low-level write-only Go binding around a QRL contract.
type EventEmitterTransactorRaw struct {
	Contract *EventEmitterTransactor // Generic write-only contract binding to access the raw methods on
}

// NewEventEmitter creates a new instance of EventEmitter, bound to a specific deployed contract.
func NewEventEmitter(address common.Address, backend bind.ContractBackend) (*EventEmitter, error) {
	contract, err := bindEventEmitter(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &EventEmitter{EventEmitterCaller: EventEmitterCaller{contract: contract}, EventEmitterTransactor: EventEmitterTransactor{contract: contract}, EventEmitterFilterer: EventEmitterFilterer{contract: contract}}, nil
}

// NewEventEmitterCaller creates a new read-only instance of EventEmitter, bound to a specific deployed contract.
func NewEventEmitterCaller(address common.Address, caller bind.ContractCaller) (*EventEmitterCaller, error) {
	contract, err := bindEventEmitter(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &EventEmitterCaller{contract: contract}, nil
}

// NewEventEmitterTransactor creates a new write-only instance of EventEmitter, bound to a specific deployed contract.
func NewEventEmitterTransactor(address common.Address, transactor bind.ContractTransactor) (*EventEmitterTransactor, error) {
	contract, err := bindEventEmitter(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &EventEmitterTransactor{contract: contract}, nil
}

// NewEventEmitterFilterer creates a new log filterer instance of EventEmitter, bound to a specific deployed contract.
func NewEventEmitterFilterer(address common.Address, filterer bind.ContractFilterer) (*EventEmitterFilterer, error) {
	contract, err := bindEventEmitter(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &EventEmitterFilterer{contract: contract}, nil
}

// bindEventEmitter binds a generic wrapper to an already deployed contract.
func bindEventEmitter(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := EventEmitterMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_EventEmitter *EventEmitterRaw) Call(opts *bind.CallOpts, result *[]any, method string, params ...any) error {
	return _EventEmitter.Contract.EventEmitterCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_EventEmitter *EventEmitterRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _EventEmitter.Contract.EventEmitterTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_EventEmitter *EventEmitterRaw) Transact(opts *bind.TransactOpts, method string, params ...any) (*types.Transaction, error) {
	return _EventEmitter.Contract.EventEmitterTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_EventEmitter *EventEmitterCallerRaw) Call(opts *bind.CallOpts, result *[]any, method string, params ...any) error {
	return _EventEmitter.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_EventEmitter *EventEmitterTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _EventEmitter.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_EventEmitter *EventEmitterTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...any) (*types.Transaction, error) {
	return _EventEmitter.Contract.contract.Transact(opts, method, params...)
}

// Echo is a free data retrieval call binding the contract method 0x4b79d0e3.
//
// Hyperion: function echo(uint512 amount, int512 delta, bytes64 tag, address recipient, bytes payload, string note, bool enabled) pure returns(uint512, int512, bytes64, address, bytes, string, bool)
func (_EventEmitter *EventEmitterCaller) Echo(opts *bind.CallOpts, amount *big.Int, delta *big.Int, tag [64]byte, recipient common.Address, payload []byte, note string, enabled bool) (*big.Int, *big.Int, [64]byte, common.Address, []byte, string, bool, error) {
	var out []any
	err := _EventEmitter.contract.Call(opts, &out, "echo", amount, delta, tag, recipient, payload, note, enabled)

	if err != nil {
		return *new(*big.Int), *new(*big.Int), *new([64]byte), *new(common.Address), *new([]byte), *new(string), *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	out1 := *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	out2 := *abi.ConvertType(out[2], new([64]byte)).(*[64]byte)
	out3 := *abi.ConvertType(out[3], new(common.Address)).(*common.Address)
	out4 := *abi.ConvertType(out[4], new([]byte)).(*[]byte)
	out5 := *abi.ConvertType(out[5], new(string)).(*string)
	out6 := *abi.ConvertType(out[6], new(bool)).(*bool)

	return out0, out1, out2, out3, out4, out5, out6, err

}

// Echo is a free data retrieval call binding the contract method 0x4b79d0e3.
//
// Hyperion: function echo(uint512 amount, int512 delta, bytes64 tag, address recipient, bytes payload, string note, bool enabled) pure returns(uint512, int512, bytes64, address, bytes, string, bool)
func (_EventEmitter *EventEmitterSession) Echo(amount *big.Int, delta *big.Int, tag [64]byte, recipient common.Address, payload []byte, note string, enabled bool) (*big.Int, *big.Int, [64]byte, common.Address, []byte, string, bool, error) {
	return _EventEmitter.Contract.Echo(&_EventEmitter.CallOpts, amount, delta, tag, recipient, payload, note, enabled)
}

// Echo is a free data retrieval call binding the contract method 0x4b79d0e3.
//
// Hyperion: function echo(uint512 amount, int512 delta, bytes64 tag, address recipient, bytes payload, string note, bool enabled) pure returns(uint512, int512, bytes64, address, bytes, string, bool)
func (_EventEmitter *EventEmitterCallerSession) Echo(amount *big.Int, delta *big.Int, tag [64]byte, recipient common.Address, payload []byte, note string, enabled bool) (*big.Int, *big.Int, [64]byte, common.Address, []byte, string, bool, error) {
	return _EventEmitter.Contract.Echo(&_EventEmitter.CallOpts, amount, delta, tag, recipient, payload, note, enabled)
}

// EchoBoundaries is a free data retrieval call binding the contract method 0x3b0e4d67.
//
// Hyperion: function echoBoundaries(uint8 smallUnsigned, int8 smallSigned, uint256 wideUnsigned, int256 wideSigned, bytes5 shortBytes, uint16[3] fixedNumbers, string[2] fixedStrings, uint16[][2] mixed) pure returns(uint8, int8, uint256, int256, bytes5, uint16[3], string[2], uint16[][2])
func (_EventEmitter *EventEmitterCaller) EchoBoundaries(opts *bind.CallOpts, smallUnsigned uint8, smallSigned int8, wideUnsigned *big.Int, wideSigned *big.Int, shortBytes [5]byte, fixedNumbers [3]uint16, fixedStrings [2]string, mixed [2][]uint16) (uint8, int8, *big.Int, *big.Int, [5]byte, [3]uint16, [2]string, [2][]uint16, error) {
	var out []any
	err := _EventEmitter.contract.Call(opts, &out, "echoBoundaries", smallUnsigned, smallSigned, wideUnsigned, wideSigned, shortBytes, fixedNumbers, fixedStrings, mixed)

	if err != nil {
		return *new(uint8), *new(int8), *new(*big.Int), *new(*big.Int), *new([5]byte), *new([3]uint16), *new([2]string), *new([2][]uint16), err
	}

	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)
	out1 := *abi.ConvertType(out[1], new(int8)).(*int8)
	out2 := *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)
	out3 := *abi.ConvertType(out[3], new(*big.Int)).(**big.Int)
	out4 := *abi.ConvertType(out[4], new([5]byte)).(*[5]byte)
	out5 := *abi.ConvertType(out[5], new([3]uint16)).(*[3]uint16)
	out6 := *abi.ConvertType(out[6], new([2]string)).(*[2]string)
	out7 := *abi.ConvertType(out[7], new([2][]uint16)).(*[2][]uint16)

	return out0, out1, out2, out3, out4, out5, out6, out7, err

}

// EchoBoundaries is a free data retrieval call binding the contract method 0x3b0e4d67.
//
// Hyperion: function echoBoundaries(uint8 smallUnsigned, int8 smallSigned, uint256 wideUnsigned, int256 wideSigned, bytes5 shortBytes, uint16[3] fixedNumbers, string[2] fixedStrings, uint16[][2] mixed) pure returns(uint8, int8, uint256, int256, bytes5, uint16[3], string[2], uint16[][2])
func (_EventEmitter *EventEmitterSession) EchoBoundaries(smallUnsigned uint8, smallSigned int8, wideUnsigned *big.Int, wideSigned *big.Int, shortBytes [5]byte, fixedNumbers [3]uint16, fixedStrings [2]string, mixed [2][]uint16) (uint8, int8, *big.Int, *big.Int, [5]byte, [3]uint16, [2]string, [2][]uint16, error) {
	return _EventEmitter.Contract.EchoBoundaries(&_EventEmitter.CallOpts, smallUnsigned, smallSigned, wideUnsigned, wideSigned, shortBytes, fixedNumbers, fixedStrings, mixed)
}

// EchoBoundaries is a free data retrieval call binding the contract method 0x3b0e4d67.
//
// Hyperion: function echoBoundaries(uint8 smallUnsigned, int8 smallSigned, uint256 wideUnsigned, int256 wideSigned, bytes5 shortBytes, uint16[3] fixedNumbers, string[2] fixedStrings, uint16[][2] mixed) pure returns(uint8, int8, uint256, int256, bytes5, uint16[3], string[2], uint16[][2])
func (_EventEmitter *EventEmitterCallerSession) EchoBoundaries(smallUnsigned uint8, smallSigned int8, wideUnsigned *big.Int, wideSigned *big.Int, shortBytes [5]byte, fixedNumbers [3]uint16, fixedStrings [2]string, mixed [2][]uint16) (uint8, int8, *big.Int, *big.Int, [5]byte, [3]uint16, [2]string, [2][]uint16, error) {
	return _EventEmitter.Contract.EchoBoundaries(&_EventEmitter.CallOpts, smallUnsigned, smallSigned, wideUnsigned, wideSigned, shortBytes, fixedNumbers, fixedStrings, mixed)
}

// EchoBoundaryEdges is a free data retrieval call binding the contract method 0x4dc96ec0.
//
// Hyperion: function echoBoundaryEdges((uint248,int248,uint256,int256,uint264,int264,uint504,int504,uint512,int512,bytes31,bytes32,bytes33,bytes63,bytes64) edges) pure returns((uint248,int248,uint256,int256,uint264,int264,uint504,int504,uint512,int512,bytes31,bytes32,bytes33,bytes63,bytes64))
func (_EventEmitter *EventEmitterCaller) EchoBoundaryEdges(opts *bind.CallOpts, edges EventEmitterBoundaryEdges) (EventEmitterBoundaryEdges, error) {
	var out []any
	err := _EventEmitter.contract.Call(opts, &out, "echoBoundaryEdges", edges)

	if err != nil {
		return *new(EventEmitterBoundaryEdges), err
	}

	out0 := *abi.ConvertType(out[0], new(EventEmitterBoundaryEdges)).(*EventEmitterBoundaryEdges)

	return out0, err

}

// EchoBoundaryEdges is a free data retrieval call binding the contract method 0x4dc96ec0.
//
// Hyperion: function echoBoundaryEdges((uint248,int248,uint256,int256,uint264,int264,uint504,int504,uint512,int512,bytes31,bytes32,bytes33,bytes63,bytes64) edges) pure returns((uint248,int248,uint256,int256,uint264,int264,uint504,int504,uint512,int512,bytes31,bytes32,bytes33,bytes63,bytes64))
func (_EventEmitter *EventEmitterSession) EchoBoundaryEdges(edges EventEmitterBoundaryEdges) (EventEmitterBoundaryEdges, error) {
	return _EventEmitter.Contract.EchoBoundaryEdges(&_EventEmitter.CallOpts, edges)
}

// EchoBoundaryEdges is a free data retrieval call binding the contract method 0x4dc96ec0.
//
// Hyperion: function echoBoundaryEdges((uint248,int248,uint256,int256,uint264,int264,uint504,int504,uint512,int512,bytes31,bytes32,bytes33,bytes63,bytes64) edges) pure returns((uint248,int248,uint256,int256,uint264,int264,uint504,int504,uint512,int512,bytes31,bytes32,bytes33,bytes63,bytes64))
func (_EventEmitter *EventEmitterCallerSession) EchoBoundaryEdges(edges EventEmitterBoundaryEdges) (EventEmitterBoundaryEdges, error) {
	return _EventEmitter.Contract.EchoBoundaryEdges(&_EventEmitter.CallOpts, edges)
}

// EchoCompositeContainers is a free data retrieval call binding the contract method 0xaf73dcd1.
//
// Hyperion: function echoCompositeContainers(uint16[2][2] fixedMatrix, uint16[2][] rows, (uint512,string,bytes,uint16[][])[2] records, ((uint512,address,bytes64),(uint512,string,bytes,uint16[][]),bytes) nested) pure returns(uint16[2][2], uint16[2][], (uint512,string,bytes,uint16[][])[2], ((uint512,address,bytes64),(uint512,string,bytes,uint16[][]),bytes))
func (_EventEmitter *EventEmitterCaller) EchoCompositeContainers(opts *bind.CallOpts, fixedMatrix [2][2]uint16, rows [][2]uint16, records [2]EventEmitterDynamicRecord, nested EventEmitterNestedRecord) ([2][2]uint16, [][2]uint16, [2]EventEmitterDynamicRecord, EventEmitterNestedRecord, error) {
	var out []any
	err := _EventEmitter.contract.Call(opts, &out, "echoCompositeContainers", fixedMatrix, rows, records, nested)

	if err != nil {
		return *new([2][2]uint16), *new([][2]uint16), *new([2]EventEmitterDynamicRecord), *new(EventEmitterNestedRecord), err
	}

	out0 := *abi.ConvertType(out[0], new([2][2]uint16)).(*[2][2]uint16)
	out1 := *abi.ConvertType(out[1], new([][2]uint16)).(*[][2]uint16)
	out2 := *abi.ConvertType(out[2], new([2]EventEmitterDynamicRecord)).(*[2]EventEmitterDynamicRecord)
	out3 := *abi.ConvertType(out[3], new(EventEmitterNestedRecord)).(*EventEmitterNestedRecord)

	return out0, out1, out2, out3, err

}

// EchoCompositeContainers is a free data retrieval call binding the contract method 0xaf73dcd1.
//
// Hyperion: function echoCompositeContainers(uint16[2][2] fixedMatrix, uint16[2][] rows, (uint512,string,bytes,uint16[][])[2] records, ((uint512,address,bytes64),(uint512,string,bytes,uint16[][]),bytes) nested) pure returns(uint16[2][2], uint16[2][], (uint512,string,bytes,uint16[][])[2], ((uint512,address,bytes64),(uint512,string,bytes,uint16[][]),bytes))
func (_EventEmitter *EventEmitterSession) EchoCompositeContainers(fixedMatrix [2][2]uint16, rows [][2]uint16, records [2]EventEmitterDynamicRecord, nested EventEmitterNestedRecord) ([2][2]uint16, [][2]uint16, [2]EventEmitterDynamicRecord, EventEmitterNestedRecord, error) {
	return _EventEmitter.Contract.EchoCompositeContainers(&_EventEmitter.CallOpts, fixedMatrix, rows, records, nested)
}

// EchoCompositeContainers is a free data retrieval call binding the contract method 0xaf73dcd1.
//
// Hyperion: function echoCompositeContainers(uint16[2][2] fixedMatrix, uint16[2][] rows, (uint512,string,bytes,uint16[][])[2] records, ((uint512,address,bytes64),(uint512,string,bytes,uint16[][]),bytes) nested) pure returns(uint16[2][2], uint16[2][], (uint512,string,bytes,uint16[][])[2], ((uint512,address,bytes64),(uint512,string,bytes,uint16[][]),bytes))
func (_EventEmitter *EventEmitterCallerSession) EchoCompositeContainers(fixedMatrix [2][2]uint16, rows [][2]uint16, records [2]EventEmitterDynamicRecord, nested EventEmitterNestedRecord) ([2][2]uint16, [][2]uint16, [2]EventEmitterDynamicRecord, EventEmitterNestedRecord, error) {
	return _EventEmitter.Contract.EchoCompositeContainers(&_EventEmitter.CallOpts, fixedMatrix, rows, records, nested)
}

// EchoDynamicContainers is a free data retrieval call binding the contract method 0x50aa10c9.
//
// Hyperion: function echoDynamicContainers(bytes[2] fixedBytes, bytes[] byteSlices, string[] strings) pure returns(bytes[2], bytes[], string[])
func (_EventEmitter *EventEmitterCaller) EchoDynamicContainers(opts *bind.CallOpts, fixedBytes [2][]byte, byteSlices [][]byte, strings []string) ([2][]byte, [][]byte, []string, error) {
	var out []any
	err := _EventEmitter.contract.Call(opts, &out, "echoDynamicContainers", fixedBytes, byteSlices, strings)

	if err != nil {
		return *new([2][]byte), *new([][]byte), *new([]string), err
	}

	out0 := *abi.ConvertType(out[0], new([2][]byte)).(*[2][]byte)
	out1 := *abi.ConvertType(out[1], new([][]byte)).(*[][]byte)
	out2 := *abi.ConvertType(out[2], new([]string)).(*[]string)

	return out0, out1, out2, err

}

// EchoDynamicContainers is a free data retrieval call binding the contract method 0x50aa10c9.
//
// Hyperion: function echoDynamicContainers(bytes[2] fixedBytes, bytes[] byteSlices, string[] strings) pure returns(bytes[2], bytes[], string[])
func (_EventEmitter *EventEmitterSession) EchoDynamicContainers(fixedBytes [2][]byte, byteSlices [][]byte, strings []string) ([2][]byte, [][]byte, []string, error) {
	return _EventEmitter.Contract.EchoDynamicContainers(&_EventEmitter.CallOpts, fixedBytes, byteSlices, strings)
}

// EchoDynamicContainers is a free data retrieval call binding the contract method 0x50aa10c9.
//
// Hyperion: function echoDynamicContainers(bytes[2] fixedBytes, bytes[] byteSlices, string[] strings) pure returns(bytes[2], bytes[], string[])
func (_EventEmitter *EventEmitterCallerSession) EchoDynamicContainers(fixedBytes [2][]byte, byteSlices [][]byte, strings []string) ([2][]byte, [][]byte, []string, error) {
	return _EventEmitter.Contract.EchoDynamicContainers(&_EventEmitter.CallOpts, fixedBytes, byteSlices, strings)
}

// EchoFunctions is a free data retrieval call binding the contract method 0xe558a3a7.
//
// Hyperion: function echoFunctions(function callback, string note, function[2] fixedCallbacks, function[] callbacks, (function,string) record) pure returns(function, string, function[2], function[], (function,string))
func (_EventEmitter *EventEmitterCaller) EchoFunctions(opts *bind.CallOpts, callback [68]byte, note string, fixedCallbacks [2][68]byte, callbacks [][68]byte, record EventEmitterFunctionRecord) ([68]byte, string, [2][68]byte, [][68]byte, EventEmitterFunctionRecord, error) {
	var out []any
	err := _EventEmitter.contract.Call(opts, &out, "echoFunctions", callback, note, fixedCallbacks, callbacks, record)

	if err != nil {
		return *new([68]byte), *new(string), *new([2][68]byte), *new([][68]byte), *new(EventEmitterFunctionRecord), err
	}

	out0 := *abi.ConvertType(out[0], new([68]byte)).(*[68]byte)
	out1 := *abi.ConvertType(out[1], new(string)).(*string)
	out2 := *abi.ConvertType(out[2], new([2][68]byte)).(*[2][68]byte)
	out3 := *abi.ConvertType(out[3], new([][68]byte)).(*[][68]byte)
	out4 := *abi.ConvertType(out[4], new(EventEmitterFunctionRecord)).(*EventEmitterFunctionRecord)

	return out0, out1, out2, out3, out4, err

}

// EchoFunctions is a free data retrieval call binding the contract method 0xe558a3a7.
//
// Hyperion: function echoFunctions(function callback, string note, function[2] fixedCallbacks, function[] callbacks, (function,string) record) pure returns(function, string, function[2], function[], (function,string))
func (_EventEmitter *EventEmitterSession) EchoFunctions(callback [68]byte, note string, fixedCallbacks [2][68]byte, callbacks [][68]byte, record EventEmitterFunctionRecord) ([68]byte, string, [2][68]byte, [][68]byte, EventEmitterFunctionRecord, error) {
	return _EventEmitter.Contract.EchoFunctions(&_EventEmitter.CallOpts, callback, note, fixedCallbacks, callbacks, record)
}

// EchoFunctions is a free data retrieval call binding the contract method 0xe558a3a7.
//
// Hyperion: function echoFunctions(function callback, string note, function[2] fixedCallbacks, function[] callbacks, (function,string) record) pure returns(function, string, function[2], function[], (function,string))
func (_EventEmitter *EventEmitterCallerSession) EchoFunctions(callback [68]byte, note string, fixedCallbacks [2][68]byte, callbacks [][68]byte, record EventEmitterFunctionRecord) ([68]byte, string, [2][68]byte, [][68]byte, EventEmitterFunctionRecord, error) {
	return _EventEmitter.Contract.EchoFunctions(&_EventEmitter.CallOpts, callback, note, fixedCallbacks, callbacks, record)
}

// EchoLeafContainers is a free data retrieval call binding the contract method 0x0753c06a.
//
// Hyperion: function echoLeafContainers(address[2] fixedAddresses, address[] addresses, bytes64[2] fixedTags, bytes64[] tags) pure returns(address[2], address[], bytes64[2], bytes64[])
func (_EventEmitter *EventEmitterCaller) EchoLeafContainers(opts *bind.CallOpts, fixedAddresses [2]common.Address, addresses []common.Address, fixedTags [2][64]byte, tags [][64]byte) ([2]common.Address, []common.Address, [2][64]byte, [][64]byte, error) {
	var out []any
	err := _EventEmitter.contract.Call(opts, &out, "echoLeafContainers", fixedAddresses, addresses, fixedTags, tags)

	if err != nil {
		return *new([2]common.Address), *new([]common.Address), *new([2][64]byte), *new([][64]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([2]common.Address)).(*[2]common.Address)
	out1 := *abi.ConvertType(out[1], new([]common.Address)).(*[]common.Address)
	out2 := *abi.ConvertType(out[2], new([2][64]byte)).(*[2][64]byte)
	out3 := *abi.ConvertType(out[3], new([][64]byte)).(*[][64]byte)

	return out0, out1, out2, out3, err

}

// EchoLeafContainers is a free data retrieval call binding the contract method 0x0753c06a.
//
// Hyperion: function echoLeafContainers(address[2] fixedAddresses, address[] addresses, bytes64[2] fixedTags, bytes64[] tags) pure returns(address[2], address[], bytes64[2], bytes64[])
func (_EventEmitter *EventEmitterSession) EchoLeafContainers(fixedAddresses [2]common.Address, addresses []common.Address, fixedTags [2][64]byte, tags [][64]byte) ([2]common.Address, []common.Address, [2][64]byte, [][64]byte, error) {
	return _EventEmitter.Contract.EchoLeafContainers(&_EventEmitter.CallOpts, fixedAddresses, addresses, fixedTags, tags)
}

// EchoLeafContainers is a free data retrieval call binding the contract method 0x0753c06a.
//
// Hyperion: function echoLeafContainers(address[2] fixedAddresses, address[] addresses, bytes64[2] fixedTags, bytes64[] tags) pure returns(address[2], address[], bytes64[2], bytes64[])
func (_EventEmitter *EventEmitterCallerSession) EchoLeafContainers(fixedAddresses [2]common.Address, addresses []common.Address, fixedTags [2][64]byte, tags [][64]byte) ([2]common.Address, []common.Address, [2][64]byte, [][64]byte, error) {
	return _EventEmitter.Contract.EchoLeafContainers(&_EventEmitter.CallOpts, fixedAddresses, addresses, fixedTags, tags)
}

// EchoNested is a free data retrieval call binding the contract method 0xf8041229.
//
// Hyperion: function echoNested((uint512,string,bytes,uint16[][]) record, (uint512,string,bytes,uint16[][])[] records, uint16[][][] cube) pure returns((uint512,string,bytes,uint16[][]), (uint512,string,bytes,uint16[][])[], uint16[][][])
func (_EventEmitter *EventEmitterCaller) EchoNested(opts *bind.CallOpts, record EventEmitterDynamicRecord, records []EventEmitterDynamicRecord, cube [][][]uint16) (EventEmitterDynamicRecord, []EventEmitterDynamicRecord, [][][]uint16, error) {
	var out []any
	err := _EventEmitter.contract.Call(opts, &out, "echoNested", record, records, cube)

	if err != nil {
		return *new(EventEmitterDynamicRecord), *new([]EventEmitterDynamicRecord), *new([][][]uint16), err
	}

	out0 := *abi.ConvertType(out[0], new(EventEmitterDynamicRecord)).(*EventEmitterDynamicRecord)
	out1 := *abi.ConvertType(out[1], new([]EventEmitterDynamicRecord)).(*[]EventEmitterDynamicRecord)
	out2 := *abi.ConvertType(out[2], new([][][]uint16)).(*[][][]uint16)

	return out0, out1, out2, err

}

// EchoNested is a free data retrieval call binding the contract method 0xf8041229.
//
// Hyperion: function echoNested((uint512,string,bytes,uint16[][]) record, (uint512,string,bytes,uint16[][])[] records, uint16[][][] cube) pure returns((uint512,string,bytes,uint16[][]), (uint512,string,bytes,uint16[][])[], uint16[][][])
func (_EventEmitter *EventEmitterSession) EchoNested(record EventEmitterDynamicRecord, records []EventEmitterDynamicRecord, cube [][][]uint16) (EventEmitterDynamicRecord, []EventEmitterDynamicRecord, [][][]uint16, error) {
	return _EventEmitter.Contract.EchoNested(&_EventEmitter.CallOpts, record, records, cube)
}

// EchoNested is a free data retrieval call binding the contract method 0xf8041229.
//
// Hyperion: function echoNested((uint512,string,bytes,uint16[][]) record, (uint512,string,bytes,uint16[][])[] records, uint16[][][] cube) pure returns((uint512,string,bytes,uint16[][]), (uint512,string,bytes,uint16[][])[], uint16[][][])
func (_EventEmitter *EventEmitterCallerSession) EchoNested(record EventEmitterDynamicRecord, records []EventEmitterDynamicRecord, cube [][][]uint16) (EventEmitterDynamicRecord, []EventEmitterDynamicRecord, [][][]uint16, error) {
	return _EventEmitter.Contract.EchoNested(&_EventEmitter.CallOpts, record, records, cube)
}

// FailComplex is a free data retrieval call binding the contract method 0xfb144722.
//
// Hyperion: function failComplex(uint512 code, string reason, bytes payload, (uint512,address,bytes64) record, uint16[][] nested) pure returns()
func (_EventEmitter *EventEmitterCaller) FailComplex(opts *bind.CallOpts, code *big.Int, reason string, payload []byte, record EventEmitterRecord, nested [][]uint16) error {
	var out []any
	err := _EventEmitter.contract.Call(opts, &out, "failComplex", code, reason, payload, record, nested)

	if err != nil {
		return err
	}

	return err

}

// FailComplex is a free data retrieval call binding the contract method 0xfb144722.
//
// Hyperion: function failComplex(uint512 code, string reason, bytes payload, (uint512,address,bytes64) record, uint16[][] nested) pure returns()
func (_EventEmitter *EventEmitterSession) FailComplex(code *big.Int, reason string, payload []byte, record EventEmitterRecord, nested [][]uint16) error {
	return _EventEmitter.Contract.FailComplex(&_EventEmitter.CallOpts, code, reason, payload, record, nested)
}

// FailComplex is a free data retrieval call binding the contract method 0xfb144722.
//
// Hyperion: function failComplex(uint512 code, string reason, bytes payload, (uint512,address,bytes64) record, uint16[][] nested) pure returns()
func (_EventEmitter *EventEmitterCallerSession) FailComplex(code *big.Int, reason string, payload []byte, record EventEmitterRecord, nested [][]uint16) error {
	return _EventEmitter.Contract.FailComplex(&_EventEmitter.CallOpts, code, reason, payload, record, nested)
}

// FailHalted is a free data retrieval call binding the contract method 0x2f6077c0.
//
// Hyperion: function failHalted() pure returns()
func (_EventEmitter *EventEmitterCaller) FailHalted(opts *bind.CallOpts) error {
	var out []any
	err := _EventEmitter.contract.Call(opts, &out, "failHalted")

	if err != nil {
		return err
	}

	return err

}

// FailHalted is a free data retrieval call binding the contract method 0x2f6077c0.
//
// Hyperion: function failHalted() pure returns()
func (_EventEmitter *EventEmitterSession) FailHalted() error {
	return _EventEmitter.Contract.FailHalted(&_EventEmitter.CallOpts)
}

// FailHalted is a free data retrieval call binding the contract method 0x2f6077c0.
//
// Hyperion: function failHalted() pure returns()
func (_EventEmitter *EventEmitterCallerSession) FailHalted() error {
	return _EventEmitter.Contract.FailHalted(&_EventEmitter.CallOpts)
}

// FailPanic is a free data retrieval call binding the contract method 0xc66c9028.
//
// Hyperion: function failPanic() pure returns()
func (_EventEmitter *EventEmitterCaller) FailPanic(opts *bind.CallOpts) error {
	var out []any
	err := _EventEmitter.contract.Call(opts, &out, "failPanic")

	if err != nil {
		return err
	}

	return err

}

// FailPanic is a free data retrieval call binding the contract method 0xc66c9028.
//
// Hyperion: function failPanic() pure returns()
func (_EventEmitter *EventEmitterSession) FailPanic() error {
	return _EventEmitter.Contract.FailPanic(&_EventEmitter.CallOpts)
}

// FailPanic is a free data retrieval call binding the contract method 0xc66c9028.
//
// Hyperion: function failPanic() pure returns()
func (_EventEmitter *EventEmitterCallerSession) FailPanic() error {
	return _EventEmitter.Contract.FailPanic(&_EventEmitter.CallOpts)
}

// FailReason is a free data retrieval call binding the contract method 0xb0b75436.
//
// Hyperion: function failReason() pure returns()
func (_EventEmitter *EventEmitterCaller) FailReason(opts *bind.CallOpts) error {
	var out []any
	err := _EventEmitter.contract.Call(opts, &out, "failReason")

	if err != nil {
		return err
	}

	return err

}

// FailReason is a free data retrieval call binding the contract method 0xb0b75436.
//
// Hyperion: function failReason() pure returns()
func (_EventEmitter *EventEmitterSession) FailReason() error {
	return _EventEmitter.Contract.FailReason(&_EventEmitter.CallOpts)
}

// FailReason is a free data retrieval call binding the contract method 0xb0b75436.
//
// Hyperion: function failReason() pure returns()
func (_EventEmitter *EventEmitterCallerSession) FailReason() error {
	return _EventEmitter.Contract.FailReason(&_EventEmitter.CallOpts)
}

// Observe is a free data retrieval call binding the contract method 0x14fc78fc.
//
// Hyperion: function observe() view returns(uint512 value, address caller)
func (_EventEmitter *EventEmitterCaller) Observe(opts *bind.CallOpts) (struct {
	Value  *big.Int
	Caller common.Address
}, error) {
	var out []any
	err := _EventEmitter.contract.Call(opts, &out, "observe")

	outstruct := new(struct {
		Value  *big.Int
		Caller common.Address
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Value = *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	outstruct.Caller = *abi.ConvertType(out[1], new(common.Address)).(*common.Address)

	return *outstruct, err

}

// Observe is a free data retrieval call binding the contract method 0x14fc78fc.
//
// Hyperion: function observe() view returns(uint512 value, address caller)
func (_EventEmitter *EventEmitterSession) Observe() (struct {
	Value  *big.Int
	Caller common.Address
}, error) {
	return _EventEmitter.Contract.Observe(&_EventEmitter.CallOpts)
}

// Observe is a free data retrieval call binding the contract method 0x14fc78fc.
//
// Hyperion: function observe() view returns(uint512 value, address caller)
func (_EventEmitter *EventEmitterCallerSession) Observe() (struct {
	Value  *big.Int
	Caller common.Address
}, error) {
	return _EventEmitter.Contract.Observe(&_EventEmitter.CallOpts)
}

// PlusOne is a free data retrieval call binding the contract method 0x79531c40.
//
// Hyperion: function plusOne(uint512 value) pure returns(uint512)
func (_EventEmitter *EventEmitterCaller) PlusOne(opts *bind.CallOpts, value *big.Int) (*big.Int, error) {
	var out []any
	err := _EventEmitter.contract.Call(opts, &out, "plusOne", value)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// PlusOne is a free data retrieval call binding the contract method 0x79531c40.
//
// Hyperion: function plusOne(uint512 value) pure returns(uint512)
func (_EventEmitter *EventEmitterSession) PlusOne(value *big.Int) (*big.Int, error) {
	return _EventEmitter.Contract.PlusOne(&_EventEmitter.CallOpts, value)
}

// PlusOne is a free data retrieval call binding the contract method 0x79531c40.
//
// Hyperion: function plusOne(uint512 value) pure returns(uint512)
func (_EventEmitter *EventEmitterCallerSession) PlusOne(value *big.Int) (*big.Int, error) {
	return _EventEmitter.Contract.PlusOne(&_EventEmitter.CallOpts, value)
}

// Transform is a free data retrieval call binding the contract method 0x9e420a8f.
//
// Hyperion: function transform(string value) pure returns(string)
func (_EventEmitter *EventEmitterCaller) Transform(opts *bind.CallOpts, value string) (string, error) {
	var out []any
	err := _EventEmitter.contract.Call(opts, &out, "transform", value)

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Transform is a free data retrieval call binding the contract method 0x9e420a8f.
//
// Hyperion: function transform(string value) pure returns(string)
func (_EventEmitter *EventEmitterSession) Transform(value string) (string, error) {
	return _EventEmitter.Contract.Transform(&_EventEmitter.CallOpts, value)
}

// Transform is a free data retrieval call binding the contract method 0x9e420a8f.
//
// Hyperion: function transform(string value) pure returns(string)
func (_EventEmitter *EventEmitterCallerSession) Transform(value string) (string, error) {
	return _EventEmitter.Contract.Transform(&_EventEmitter.CallOpts, value)
}

// Transform0 is a free data retrieval call binding the contract method 0xed928c96.
//
// Hyperion: function transform(uint16 value) pure returns(uint16)
func (_EventEmitter *EventEmitterCaller) Transform0(opts *bind.CallOpts, value uint16) (uint16, error) {
	var out []any
	err := _EventEmitter.contract.Call(opts, &out, "transform0", value)

	if err != nil {
		return *new(uint16), err
	}

	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)

	return out0, err

}

// Transform0 is a free data retrieval call binding the contract method 0xed928c96.
//
// Hyperion: function transform(uint16 value) pure returns(uint16)
func (_EventEmitter *EventEmitterSession) Transform0(value uint16) (uint16, error) {
	return _EventEmitter.Contract.Transform0(&_EventEmitter.CallOpts, value)
}

// Transform0 is a free data retrieval call binding the contract method 0xed928c96.
//
// Hyperion: function transform(uint16 value) pure returns(uint16)
func (_EventEmitter *EventEmitterCallerSession) Transform0(value uint16) (uint16, error) {
	return _EventEmitter.Contract.Transform0(&_EventEmitter.CallOpts, value)
}

// EmitComposite is a paid mutator transaction binding the contract method 0x99cf235f.
//
// Hyperion: function emitComposite((uint512,string,bytes,uint16[][]) record, uint16[3] fixedNumbers, string[2] fixedStrings, uint16[][2] mixed) returns()
func (_EventEmitter *EventEmitterTransactor) EmitComposite(opts *bind.TransactOpts, record EventEmitterDynamicRecord, fixedNumbers [3]uint16, fixedStrings [2]string, mixed [2][]uint16) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "emitComposite", record, fixedNumbers, fixedStrings, mixed)
}

// EmitComposite is a paid mutator transaction binding the contract method 0x99cf235f.
//
// Hyperion: function emitComposite((uint512,string,bytes,uint16[][]) record, uint16[3] fixedNumbers, string[2] fixedStrings, uint16[][2] mixed) returns()
func (_EventEmitter *EventEmitterSession) EmitComposite(record EventEmitterDynamicRecord, fixedNumbers [3]uint16, fixedStrings [2]string, mixed [2][]uint16) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitComposite(&_EventEmitter.TransactOpts, record, fixedNumbers, fixedStrings, mixed)
}

// EmitComposite is a paid mutator transaction binding the contract method 0x99cf235f.
//
// Hyperion: function emitComposite((uint512,string,bytes,uint16[][]) record, uint16[3] fixedNumbers, string[2] fixedStrings, uint16[][2] mixed) returns()
func (_EventEmitter *EventEmitterTransactorSession) EmitComposite(record EventEmitterDynamicRecord, fixedNumbers [3]uint16, fixedStrings [2]string, mixed [2][]uint16) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitComposite(&_EventEmitter.TransactOpts, record, fixedNumbers, fixedStrings, mixed)
}

// EmitIndexedScalars is a paid mutator transaction binding the contract method 0xb94d6fa6.
//
// Hyperion: function emitIndexedScalars(bool flag, bytes5 code, int16 delta) returns()
func (_EventEmitter *EventEmitterTransactor) EmitIndexedScalars(opts *bind.TransactOpts, flag bool, code [5]byte, delta int16) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "emitIndexedScalars", flag, code, delta)
}

// EmitIndexedScalars is a paid mutator transaction binding the contract method 0xb94d6fa6.
//
// Hyperion: function emitIndexedScalars(bool flag, bytes5 code, int16 delta) returns()
func (_EventEmitter *EventEmitterSession) EmitIndexedScalars(flag bool, code [5]byte, delta int16) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitIndexedScalars(&_EventEmitter.TransactOpts, flag, code, delta)
}

// EmitIndexedScalars is a paid mutator transaction binding the contract method 0xb94d6fa6.
//
// Hyperion: function emitIndexedScalars(bool flag, bytes5 code, int16 delta) returns()
func (_EventEmitter *EventEmitterTransactorSession) EmitIndexedScalars(flag bool, code [5]byte, delta int16) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitIndexedScalars(&_EventEmitter.TransactOpts, flag, code, delta)
}

// EmitPinged is a paid mutator transaction binding the contract method 0xb2c83e2c.
//
// Hyperion: function emitPinged(uint16 marker, uint512 value) returns()
func (_EventEmitter *EventEmitterTransactor) EmitPinged(opts *bind.TransactOpts, marker uint16, value *big.Int) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "emitPinged", marker, value)
}

// EmitPinged is a paid mutator transaction binding the contract method 0xb2c83e2c.
//
// Hyperion: function emitPinged(uint16 marker, uint512 value) returns()
func (_EventEmitter *EventEmitterSession) EmitPinged(marker uint16, value *big.Int) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitPinged(&_EventEmitter.TransactOpts, marker, value)
}

// EmitPinged is a paid mutator transaction binding the contract method 0xb2c83e2c.
//
// Hyperion: function emitPinged(uint16 marker, uint512 value) returns()
func (_EventEmitter *EventEmitterTransactorSession) EmitPinged(marker uint16, value *big.Int) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitPinged(&_EventEmitter.TransactOpts, marker, value)
}

// EmitRecordSeen is a paid mutator transaction binding the contract method 0x572a647e.
//
// Hyperion: function emitRecordSeen((uint512,address,bytes64) record) returns()
func (_EventEmitter *EventEmitterTransactor) EmitRecordSeen(opts *bind.TransactOpts, record EventEmitterRecord) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "emitRecordSeen", record)
}

// EmitRecordSeen is a paid mutator transaction binding the contract method 0x572a647e.
//
// Hyperion: function emitRecordSeen((uint512,address,bytes64) record) returns()
func (_EventEmitter *EventEmitterSession) EmitRecordSeen(record EventEmitterRecord) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitRecordSeen(&_EventEmitter.TransactOpts, record)
}

// EmitRecordSeen is a paid mutator transaction binding the contract method 0x572a647e.
//
// Hyperion: function emitRecordSeen((uint512,address,bytes64) record) returns()
func (_EventEmitter *EventEmitterTransactorSession) EmitRecordSeen(record EventEmitterRecord) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitRecordSeen(&_EventEmitter.TransactOpts, record)
}

// EmitTransformed is a paid mutator transaction binding the contract method 0x1e3ed7e4.
//
// Hyperion: function emitTransformed(string value) returns()
func (_EventEmitter *EventEmitterTransactor) EmitTransformed(opts *bind.TransactOpts, value string) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "emitTransformed", value)
}

// EmitTransformed is a paid mutator transaction binding the contract method 0x1e3ed7e4.
//
// Hyperion: function emitTransformed(string value) returns()
func (_EventEmitter *EventEmitterSession) EmitTransformed(value string) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitTransformed(&_EventEmitter.TransactOpts, value)
}

// EmitTransformed is a paid mutator transaction binding the contract method 0x1e3ed7e4.
//
// Hyperion: function emitTransformed(string value) returns()
func (_EventEmitter *EventEmitterTransactorSession) EmitTransformed(value string) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitTransformed(&_EventEmitter.TransactOpts, value)
}

// EmitTransformed0 is a paid mutator transaction binding the contract method 0xf404ae99.
//
// Hyperion: function emitTransformed(uint16 value) returns()
func (_EventEmitter *EventEmitterTransactor) EmitTransformed0(opts *bind.TransactOpts, value uint16) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "emitTransformed0", value)
}

// EmitTransformed0 is a paid mutator transaction binding the contract method 0xf404ae99.
//
// Hyperion: function emitTransformed(uint16 value) returns()
func (_EventEmitter *EventEmitterSession) EmitTransformed0(value uint16) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitTransformed0(&_EventEmitter.TransactOpts, value)
}

// EmitTransformed0 is a paid mutator transaction binding the contract method 0xf404ae99.
//
// Hyperion: function emitTransformed(uint16 value) returns()
func (_EventEmitter *EventEmitterTransactorSession) EmitTransformed0(value uint16) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitTransformed0(&_EventEmitter.TransactOpts, value)
}

// ExerciseFunction is a paid mutator transaction binding the contract method 0xa43e73c9.
//
// Hyperion: function exerciseFunction(function callback, uint512 value) returns(function, uint512)
func (_EventEmitter *EventEmitterTransactor) ExerciseFunction(opts *bind.TransactOpts, callback [68]byte, value *big.Int) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "exerciseFunction", callback, value)
}

// ExerciseFunction is a paid mutator transaction binding the contract method 0xa43e73c9.
//
// Hyperion: function exerciseFunction(function callback, uint512 value) returns(function, uint512)
func (_EventEmitter *EventEmitterSession) ExerciseFunction(callback [68]byte, value *big.Int) (*types.Transaction, error) {
	return _EventEmitter.Contract.ExerciseFunction(&_EventEmitter.TransactOpts, callback, value)
}

// ExerciseFunction is a paid mutator transaction binding the contract method 0xa43e73c9.
//
// Hyperion: function exerciseFunction(function callback, uint512 value) returns(function, uint512)
func (_EventEmitter *EventEmitterTransactorSession) ExerciseFunction(callback [68]byte, value *big.Int) (*types.Transaction, error) {
	return _EventEmitter.Contract.ExerciseFunction(&_EventEmitter.TransactOpts, callback, value)
}

// Pay is a paid mutator transaction binding the contract method 0x2fb0dbcd.
//
// Hyperion: function pay(uint16 marker) payable returns()
func (_EventEmitter *EventEmitterTransactor) Pay(opts *bind.TransactOpts, marker uint16) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "pay", marker)
}

// Pay is a paid mutator transaction binding the contract method 0x2fb0dbcd.
//
// Hyperion: function pay(uint16 marker) payable returns()
func (_EventEmitter *EventEmitterSession) Pay(marker uint16) (*types.Transaction, error) {
	return _EventEmitter.Contract.Pay(&_EventEmitter.TransactOpts, marker)
}

// Pay is a paid mutator transaction binding the contract method 0x2fb0dbcd.
//
// Hyperion: function pay(uint16 marker) payable returns()
func (_EventEmitter *EventEmitterTransactorSession) Pay(marker uint16) (*types.Transaction, error) {
	return _EventEmitter.Contract.Pay(&_EventEmitter.TransactOpts, marker)
}

// Store is a paid mutator transaction binding the contract method 0x3d0e1089.
//
// Hyperion: function store(uint512 amount, int512 delta, bytes64 tag, address recipient, bytes payload, string note, bool enabled) returns()
func (_EventEmitter *EventEmitterTransactor) Store(opts *bind.TransactOpts, amount *big.Int, delta *big.Int, tag [64]byte, recipient common.Address, payload []byte, note string, enabled bool) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "store", amount, delta, tag, recipient, payload, note, enabled)
}

// Store is a paid mutator transaction binding the contract method 0x3d0e1089.
//
// Hyperion: function store(uint512 amount, int512 delta, bytes64 tag, address recipient, bytes payload, string note, bool enabled) returns()
func (_EventEmitter *EventEmitterSession) Store(amount *big.Int, delta *big.Int, tag [64]byte, recipient common.Address, payload []byte, note string, enabled bool) (*types.Transaction, error) {
	return _EventEmitter.Contract.Store(&_EventEmitter.TransactOpts, amount, delta, tag, recipient, payload, note, enabled)
}

// Store is a paid mutator transaction binding the contract method 0x3d0e1089.
//
// Hyperion: function store(uint512 amount, int512 delta, bytes64 tag, address recipient, bytes payload, string note, bool enabled) returns()
func (_EventEmitter *EventEmitterTransactorSession) Store(amount *big.Int, delta *big.Int, tag [64]byte, recipient common.Address, payload []byte, note string, enabled bool) (*types.Transaction, error) {
	return _EventEmitter.Contract.Store(&_EventEmitter.TransactOpts, amount, delta, tag, recipient, payload, note, enabled)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Hyperion: fallback() payable returns()
func (_EventEmitter *EventEmitterTransactor) Fallback(opts *bind.TransactOpts, calldata []byte) (*types.Transaction, error) {
	return _EventEmitter.contract.RawTransact(opts, calldata)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Hyperion: fallback() payable returns()
func (_EventEmitter *EventEmitterSession) Fallback(calldata []byte) (*types.Transaction, error) {
	return _EventEmitter.Contract.Fallback(&_EventEmitter.TransactOpts, calldata)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Hyperion: fallback() payable returns()
func (_EventEmitter *EventEmitterTransactorSession) Fallback(calldata []byte) (*types.Transaction, error) {
	return _EventEmitter.Contract.Fallback(&_EventEmitter.TransactOpts, calldata)
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Hyperion: receive() payable returns()
func (_EventEmitter *EventEmitterTransactor) Receive(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _EventEmitter.contract.RawTransact(opts, nil) // calldata is disallowed for receive function
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Hyperion: receive() payable returns()
func (_EventEmitter *EventEmitterSession) Receive() (*types.Transaction, error) {
	return _EventEmitter.Contract.Receive(&_EventEmitter.TransactOpts)
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Hyperion: receive() payable returns()
func (_EventEmitter *EventEmitterTransactorSession) Receive() (*types.Transaction, error) {
	return _EventEmitter.Contract.Receive(&_EventEmitter.TransactOpts)
}

// EventEmitterCompositeIterator is returned from FilterComposite and is used to iterate over the raw logs and unpacked data for Composite events raised by the EventEmitter contract.
type EventEmitterCompositeIterator struct {
	Event *EventEmitterComposite // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log   // Log channel receiving the found contract events
	sub  qrl.Subscription // Subscription for errors, completion and termination
	done bool             // Whether the subscription completed delivering logs
	fail error            // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EventEmitterCompositeIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterComposite)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EventEmitterComposite)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EventEmitterCompositeIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterCompositeIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterComposite represents a Composite event raised by the EventEmitter contract.
type EventEmitterComposite struct {
	Record       EventEmitterDynamicRecord
	FixedNumbers [3]uint16
	FixedStrings [2]string
	Mixed        [2][]uint16
	Raw          types.Log // Blockchain specific contextual infos
}

// FilterComposite is a free log retrieval operation binding the contract event 0xa5d75a0c1afe7b82fadd41d2ad66b9fb0934d4aa36a730e5da8b22ae04352e0e.
//
// Hyperion: event Composite((uint512,string,bytes,uint16[][]) record, uint16[3] fixedNumbers, string[2] fixedStrings, uint16[][2] mixed)
func (_EventEmitter *EventEmitterFilterer) FilterComposite(opts *bind.FilterOpts) (*EventEmitterCompositeIterator, error) {

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "Composite")
	if err != nil {
		return nil, err
	}
	return &EventEmitterCompositeIterator{contract: _EventEmitter.contract, event: "Composite", logs: logs, sub: sub}, nil
}

// WatchComposite is a free log subscription operation binding the contract event 0xa5d75a0c1afe7b82fadd41d2ad66b9fb0934d4aa36a730e5da8b22ae04352e0e.
//
// Hyperion: event Composite((uint512,string,bytes,uint16[][]) record, uint16[3] fixedNumbers, string[2] fixedStrings, uint16[][2] mixed)
func (_EventEmitter *EventEmitterFilterer) WatchComposite(opts *bind.WatchOpts, sink chan<- *EventEmitterComposite) (event.Subscription, error) {

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "Composite")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterComposite)
				if err := _EventEmitter.contract.UnpackLog(event, "Composite", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseComposite is a log parse operation binding the contract event 0xa5d75a0c1afe7b82fadd41d2ad66b9fb0934d4aa36a730e5da8b22ae04352e0e.
//
// Hyperion: event Composite((uint512,string,bytes,uint16[][]) record, uint16[3] fixedNumbers, string[2] fixedStrings, uint16[][2] mixed)
func (_EventEmitter *EventEmitterFilterer) ParseComposite(log types.Log) (*EventEmitterComposite, error) {
	event := new(EventEmitterComposite)
	if err := _EventEmitter.contract.UnpackLog(event, "Composite", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterDeployedIterator is returned from FilterDeployed and is used to iterate over the raw logs and unpacked data for Deployed events raised by the EventEmitter contract.
type EventEmitterDeployedIterator struct {
	Event *EventEmitterDeployed // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log   // Log channel receiving the found contract events
	sub  qrl.Subscription // Subscription for errors, completion and termination
	done bool             // Whether the subscription completed delivering logs
	fail error            // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EventEmitterDeployedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterDeployed)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EventEmitterDeployed)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EventEmitterDeployedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterDeployedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterDeployed represents a Deployed event raised by the EventEmitter contract.
type EventEmitterDeployed struct {
	Value   *big.Int
	Note    string
	Payload []byte
	Record  EventEmitterRecord
	Numbers []uint16
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterDeployed is a free log retrieval operation binding the contract event 0x100ec3f67dbb9bac62c3c46aa9e2acda3788c474549e77c16e730ee2d023be00.
//
// Hyperion: event Deployed(uint512 value, string note, bytes payload, (uint512,address,bytes64) record, uint16[] numbers)
func (_EventEmitter *EventEmitterFilterer) FilterDeployed(opts *bind.FilterOpts) (*EventEmitterDeployedIterator, error) {

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "Deployed")
	if err != nil {
		return nil, err
	}
	return &EventEmitterDeployedIterator{contract: _EventEmitter.contract, event: "Deployed", logs: logs, sub: sub}, nil
}

// WatchDeployed is a free log subscription operation binding the contract event 0x100ec3f67dbb9bac62c3c46aa9e2acda3788c474549e77c16e730ee2d023be00.
//
// Hyperion: event Deployed(uint512 value, string note, bytes payload, (uint512,address,bytes64) record, uint16[] numbers)
func (_EventEmitter *EventEmitterFilterer) WatchDeployed(opts *bind.WatchOpts, sink chan<- *EventEmitterDeployed) (event.Subscription, error) {

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "Deployed")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterDeployed)
				if err := _EventEmitter.contract.UnpackLog(event, "Deployed", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseDeployed is a log parse operation binding the contract event 0x100ec3f67dbb9bac62c3c46aa9e2acda3788c474549e77c16e730ee2d023be00.
//
// Hyperion: event Deployed(uint512 value, string note, bytes payload, (uint512,address,bytes64) record, uint16[] numbers)
func (_EventEmitter *EventEmitterFilterer) ParseDeployed(log types.Log) (*EventEmitterDeployed, error) {
	event := new(EventEmitterDeployed)
	if err := _EventEmitter.contract.UnpackLog(event, "Deployed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterDynamicIterator is returned from FilterDynamic and is used to iterate over the raw logs and unpacked data for Dynamic events raised by the EventEmitter contract.
type EventEmitterDynamicIterator struct {
	Event *EventEmitterDynamic // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log   // Log channel receiving the found contract events
	sub  qrl.Subscription // Subscription for errors, completion and termination
	done bool             // Whether the subscription completed delivering logs
	fail error            // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EventEmitterDynamicIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterDynamic)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EventEmitterDynamic)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EventEmitterDynamicIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterDynamicIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterDynamic represents a Dynamic event raised by the EventEmitter contract.
type EventEmitterDynamic struct {
	Payload common.Hash
	Note    common.Hash
	Amount  *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterDynamic is a free log retrieval operation binding the contract event 0x4ef7447df163d4aaeab9c66fa93651de5eebb002dcf9b60da1ebaa28ae95e825.
//
// Hyperion: event Dynamic(bytes indexed payload, string indexed note, uint512 amount)
func (_EventEmitter *EventEmitterFilterer) FilterDynamic(opts *bind.FilterOpts, payload [][]byte, note []string) (*EventEmitterDynamicIterator, error) {

	var payloadRule []any
	for _, payloadItem := range payload {
		payloadRule = append(payloadRule, payloadItem)
	}
	var noteRule []any
	for _, noteItem := range note {
		noteRule = append(noteRule, noteItem)
	}

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "Dynamic", payloadRule, noteRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterDynamicIterator{contract: _EventEmitter.contract, event: "Dynamic", logs: logs, sub: sub}, nil
}

// WatchDynamic is a free log subscription operation binding the contract event 0x4ef7447df163d4aaeab9c66fa93651de5eebb002dcf9b60da1ebaa28ae95e825.
//
// Hyperion: event Dynamic(bytes indexed payload, string indexed note, uint512 amount)
func (_EventEmitter *EventEmitterFilterer) WatchDynamic(opts *bind.WatchOpts, sink chan<- *EventEmitterDynamic, payload [][]byte, note []string) (event.Subscription, error) {

	var payloadRule []any
	for _, payloadItem := range payload {
		payloadRule = append(payloadRule, payloadItem)
	}
	var noteRule []any
	for _, noteItem := range note {
		noteRule = append(noteRule, noteItem)
	}

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "Dynamic", payloadRule, noteRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterDynamic)
				if err := _EventEmitter.contract.UnpackLog(event, "Dynamic", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseDynamic is a log parse operation binding the contract event 0x4ef7447df163d4aaeab9c66fa93651de5eebb002dcf9b60da1ebaa28ae95e825.
//
// Hyperion: event Dynamic(bytes indexed payload, string indexed note, uint512 amount)
func (_EventEmitter *EventEmitterFilterer) ParseDynamic(log types.Log) (*EventEmitterDynamic, error) {
	event := new(EventEmitterDynamic)
	if err := _EventEmitter.contract.UnpackLog(event, "Dynamic", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterFallbackCalledIterator is returned from FilterFallbackCalled and is used to iterate over the raw logs and unpacked data for FallbackCalled events raised by the EventEmitter contract.
type EventEmitterFallbackCalledIterator struct {
	Event *EventEmitterFallbackCalled // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log   // Log channel receiving the found contract events
	sub  qrl.Subscription // Subscription for errors, completion and termination
	done bool             // Whether the subscription completed delivering logs
	fail error            // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EventEmitterFallbackCalledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterFallbackCalled)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EventEmitterFallbackCalled)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EventEmitterFallbackCalledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterFallbackCalledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterFallbackCalled represents a FallbackCalled event raised by the EventEmitter contract.
type EventEmitterFallbackCalled struct {
	Payload []byte
	Amount  *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterFallbackCalled is a free log retrieval operation binding the contract event 0xe5b92b8ba08394dd9b027fafca0dc888f149e8f420b55893ecee14ea148aa08b.
//
// Hyperion: event FallbackCalled(bytes payload, uint256 amount)
func (_EventEmitter *EventEmitterFilterer) FilterFallbackCalled(opts *bind.FilterOpts) (*EventEmitterFallbackCalledIterator, error) {

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "FallbackCalled")
	if err != nil {
		return nil, err
	}
	return &EventEmitterFallbackCalledIterator{contract: _EventEmitter.contract, event: "FallbackCalled", logs: logs, sub: sub}, nil
}

// WatchFallbackCalled is a free log subscription operation binding the contract event 0xe5b92b8ba08394dd9b027fafca0dc888f149e8f420b55893ecee14ea148aa08b.
//
// Hyperion: event FallbackCalled(bytes payload, uint256 amount)
func (_EventEmitter *EventEmitterFilterer) WatchFallbackCalled(opts *bind.WatchOpts, sink chan<- *EventEmitterFallbackCalled) (event.Subscription, error) {

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "FallbackCalled")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterFallbackCalled)
				if err := _EventEmitter.contract.UnpackLog(event, "FallbackCalled", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFallbackCalled is a log parse operation binding the contract event 0xe5b92b8ba08394dd9b027fafca0dc888f149e8f420b55893ecee14ea148aa08b.
//
// Hyperion: event FallbackCalled(bytes payload, uint256 amount)
func (_EventEmitter *EventEmitterFilterer) ParseFallbackCalled(log types.Log) (*EventEmitterFallbackCalled, error) {
	event := new(EventEmitterFallbackCalled)
	if err := _EventEmitter.contract.UnpackLog(event, "FallbackCalled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterFunctionObservedIterator is returned from FilterFunctionObserved and is used to iterate over the raw logs and unpacked data for FunctionObserved events raised by the EventEmitter contract.
type EventEmitterFunctionObservedIterator struct {
	Event *EventEmitterFunctionObserved // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log   // Log channel receiving the found contract events
	sub  qrl.Subscription // Subscription for errors, completion and termination
	done bool             // Whether the subscription completed delivering logs
	fail error            // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EventEmitterFunctionObservedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterFunctionObserved)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EventEmitterFunctionObserved)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EventEmitterFunctionObservedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterFunctionObservedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterFunctionObserved represents a FunctionObserved event raised by the EventEmitter contract.
type EventEmitterFunctionObserved struct {
	IndexedCallback common.Hash
	Callback        [68]byte
	Result          *big.Int
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterFunctionObserved is a free log retrieval operation binding the contract event 0xfa178067c55becdc50555038a0302191054dc26036b1e5ad5d3d0a9daa93423c.
//
// Hyperion: event FunctionObserved(function indexed indexedCallback, function callback, uint512 result)
func (_EventEmitter *EventEmitterFilterer) FilterFunctionObserved(opts *bind.FilterOpts, indexedCallback [][68]byte) (*EventEmitterFunctionObservedIterator, error) {

	var indexedCallbackRule []any
	for _, indexedCallbackItem := range indexedCallback {
		indexedCallbackRule = append(indexedCallbackRule, indexedCallbackItem)
	}

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "FunctionObserved", indexedCallbackRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterFunctionObservedIterator{contract: _EventEmitter.contract, event: "FunctionObserved", logs: logs, sub: sub}, nil
}

// WatchFunctionObserved is a free log subscription operation binding the contract event 0xfa178067c55becdc50555038a0302191054dc26036b1e5ad5d3d0a9daa93423c.
//
// Hyperion: event FunctionObserved(function indexed indexedCallback, function callback, uint512 result)
func (_EventEmitter *EventEmitterFilterer) WatchFunctionObserved(opts *bind.WatchOpts, sink chan<- *EventEmitterFunctionObserved, indexedCallback [][68]byte) (event.Subscription, error) {

	var indexedCallbackRule []any
	for _, indexedCallbackItem := range indexedCallback {
		indexedCallbackRule = append(indexedCallbackRule, indexedCallbackItem)
	}

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "FunctionObserved", indexedCallbackRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterFunctionObserved)
				if err := _EventEmitter.contract.UnpackLog(event, "FunctionObserved", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFunctionObserved is a log parse operation binding the contract event 0xfa178067c55becdc50555038a0302191054dc26036b1e5ad5d3d0a9daa93423c.
//
// Hyperion: event FunctionObserved(function indexed indexedCallback, function callback, uint512 result)
func (_EventEmitter *EventEmitterFilterer) ParseFunctionObserved(log types.Log) (*EventEmitterFunctionObserved, error) {
	event := new(EventEmitterFunctionObserved)
	if err := _EventEmitter.contract.UnpackLog(event, "FunctionObserved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterIndexedScalarsIterator is returned from FilterIndexedScalars and is used to iterate over the raw logs and unpacked data for IndexedScalars events raised by the EventEmitter contract.
type EventEmitterIndexedScalarsIterator struct {
	Event *EventEmitterIndexedScalars // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log   // Log channel receiving the found contract events
	sub  qrl.Subscription // Subscription for errors, completion and termination
	done bool             // Whether the subscription completed delivering logs
	fail error            // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EventEmitterIndexedScalarsIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterIndexedScalars)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EventEmitterIndexedScalars)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EventEmitterIndexedScalarsIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterIndexedScalarsIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterIndexedScalars represents a IndexedScalars event raised by the EventEmitter contract.
type EventEmitterIndexedScalars struct {
	Flag  bool
	Code  [5]byte
	Delta int16
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterIndexedScalars is a free log retrieval operation binding the contract event 0x19c59af463d0b89e6afb02db53c6ea998a04ce7bf1aa5c2c0d4c3ac9efc9e659.
//
// Hyperion: event IndexedScalars(bool indexed flag, bytes5 indexed code, int16 indexed delta)
func (_EventEmitter *EventEmitterFilterer) FilterIndexedScalars(opts *bind.FilterOpts, flag []bool, code [][5]byte, delta []int16) (*EventEmitterIndexedScalarsIterator, error) {

	var flagRule []any
	for _, flagItem := range flag {
		flagRule = append(flagRule, flagItem)
	}
	var codeRule []any
	for _, codeItem := range code {
		codeRule = append(codeRule, codeItem)
	}
	var deltaRule []any
	for _, deltaItem := range delta {
		deltaRule = append(deltaRule, deltaItem)
	}

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "IndexedScalars", flagRule, codeRule, deltaRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterIndexedScalarsIterator{contract: _EventEmitter.contract, event: "IndexedScalars", logs: logs, sub: sub}, nil
}

// WatchIndexedScalars is a free log subscription operation binding the contract event 0x19c59af463d0b89e6afb02db53c6ea998a04ce7bf1aa5c2c0d4c3ac9efc9e659.
//
// Hyperion: event IndexedScalars(bool indexed flag, bytes5 indexed code, int16 indexed delta)
func (_EventEmitter *EventEmitterFilterer) WatchIndexedScalars(opts *bind.WatchOpts, sink chan<- *EventEmitterIndexedScalars, flag []bool, code [][5]byte, delta []int16) (event.Subscription, error) {

	var flagRule []any
	for _, flagItem := range flag {
		flagRule = append(flagRule, flagItem)
	}
	var codeRule []any
	for _, codeItem := range code {
		codeRule = append(codeRule, codeItem)
	}
	var deltaRule []any
	for _, deltaItem := range delta {
		deltaRule = append(deltaRule, deltaItem)
	}

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "IndexedScalars", flagRule, codeRule, deltaRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterIndexedScalars)
				if err := _EventEmitter.contract.UnpackLog(event, "IndexedScalars", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseIndexedScalars is a log parse operation binding the contract event 0x19c59af463d0b89e6afb02db53c6ea998a04ce7bf1aa5c2c0d4c3ac9efc9e659.
//
// Hyperion: event IndexedScalars(bool indexed flag, bytes5 indexed code, int16 indexed delta)
func (_EventEmitter *EventEmitterFilterer) ParseIndexedScalars(log types.Log) (*EventEmitterIndexedScalars, error) {
	event := new(EventEmitterIndexedScalars)
	if err := _EventEmitter.contract.UnpackLog(event, "IndexedScalars", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterPaidIterator is returned from FilterPaid and is used to iterate over the raw logs and unpacked data for Paid events raised by the EventEmitter contract.
type EventEmitterPaidIterator struct {
	Event *EventEmitterPaid // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log   // Log channel receiving the found contract events
	sub  qrl.Subscription // Subscription for errors, completion and termination
	done bool             // Whether the subscription completed delivering logs
	fail error            // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EventEmitterPaidIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterPaid)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EventEmitterPaid)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EventEmitterPaidIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterPaidIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterPaid represents a Paid event raised by the EventEmitter contract.
type EventEmitterPaid struct {
	Sender common.Address
	Marker uint16
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterPaid is a free log retrieval operation binding the contract event 0x1398d89bb96c43f8c16ef74dee904b456a4fa8a5857191293b848ced1997a3d9.
//
// Hyperion: event Paid(address indexed sender, uint16 indexed marker, uint256 amount)
func (_EventEmitter *EventEmitterFilterer) FilterPaid(opts *bind.FilterOpts, sender []common.Address, marker []uint16) (*EventEmitterPaidIterator, error) {

	var senderRule []any
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}
	var markerRule []any
	for _, markerItem := range marker {
		markerRule = append(markerRule, markerItem)
	}

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "Paid", senderRule, markerRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterPaidIterator{contract: _EventEmitter.contract, event: "Paid", logs: logs, sub: sub}, nil
}

// WatchPaid is a free log subscription operation binding the contract event 0x1398d89bb96c43f8c16ef74dee904b456a4fa8a5857191293b848ced1997a3d9.
//
// Hyperion: event Paid(address indexed sender, uint16 indexed marker, uint256 amount)
func (_EventEmitter *EventEmitterFilterer) WatchPaid(opts *bind.WatchOpts, sink chan<- *EventEmitterPaid, sender []common.Address, marker []uint16) (event.Subscription, error) {

	var senderRule []any
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}
	var markerRule []any
	for _, markerItem := range marker {
		markerRule = append(markerRule, markerItem)
	}

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "Paid", senderRule, markerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterPaid)
				if err := _EventEmitter.contract.UnpackLog(event, "Paid", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParsePaid is a log parse operation binding the contract event 0x1398d89bb96c43f8c16ef74dee904b456a4fa8a5857191293b848ced1997a3d9.
//
// Hyperion: event Paid(address indexed sender, uint16 indexed marker, uint256 amount)
func (_EventEmitter *EventEmitterFilterer) ParsePaid(log types.Log) (*EventEmitterPaid, error) {
	event := new(EventEmitterPaid)
	if err := _EventEmitter.contract.UnpackLog(event, "Paid", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterReceivedIterator is returned from FilterReceived and is used to iterate over the raw logs and unpacked data for Received events raised by the EventEmitter contract.
type EventEmitterReceivedIterator struct {
	Event *EventEmitterReceived // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log   // Log channel receiving the found contract events
	sub  qrl.Subscription // Subscription for errors, completion and termination
	done bool             // Whether the subscription completed delivering logs
	fail error            // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EventEmitterReceivedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterReceived)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EventEmitterReceived)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EventEmitterReceivedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterReceivedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterReceived represents a Received event raised by the EventEmitter contract.
type EventEmitterReceived struct {
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterReceived is a free log retrieval operation binding the contract event 0xa8142743f8f70a4c26f3691cf4ed59718381fb2f18070ec52be1f1022d855557.
//
// Hyperion: event Received(uint256 amount)
func (_EventEmitter *EventEmitterFilterer) FilterReceived(opts *bind.FilterOpts) (*EventEmitterReceivedIterator, error) {

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "Received")
	if err != nil {
		return nil, err
	}
	return &EventEmitterReceivedIterator{contract: _EventEmitter.contract, event: "Received", logs: logs, sub: sub}, nil
}

// WatchReceived is a free log subscription operation binding the contract event 0xa8142743f8f70a4c26f3691cf4ed59718381fb2f18070ec52be1f1022d855557.
//
// Hyperion: event Received(uint256 amount)
func (_EventEmitter *EventEmitterFilterer) WatchReceived(opts *bind.WatchOpts, sink chan<- *EventEmitterReceived) (event.Subscription, error) {

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "Received")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterReceived)
				if err := _EventEmitter.contract.UnpackLog(event, "Received", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseReceived is a log parse operation binding the contract event 0xa8142743f8f70a4c26f3691cf4ed59718381fb2f18070ec52be1f1022d855557.
//
// Hyperion: event Received(uint256 amount)
func (_EventEmitter *EventEmitterFilterer) ParseReceived(log types.Log) (*EventEmitterReceived, error) {
	event := new(EventEmitterReceived)
	if err := _EventEmitter.contract.UnpackLog(event, "Received", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterRecordSeenIterator is returned from FilterRecordSeen and is used to iterate over the raw logs and unpacked data for RecordSeen events raised by the EventEmitter contract.
type EventEmitterRecordSeenIterator struct {
	Event *EventEmitterRecordSeen // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log   // Log channel receiving the found contract events
	sub  qrl.Subscription // Subscription for errors, completion and termination
	done bool             // Whether the subscription completed delivering logs
	fail error            // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EventEmitterRecordSeenIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterRecordSeen)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EventEmitterRecordSeen)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EventEmitterRecordSeenIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterRecordSeenIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterRecordSeen represents a RecordSeen event raised by the EventEmitter contract.
type EventEmitterRecordSeen struct {
	Record EventEmitterRecord
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterRecordSeen is a free log retrieval operation binding the contract event 0x58adb069ff9511033daf66a044c68afaf5ee830f0543ae234db74ca220fe4c12.
//
// Hyperion: event RecordSeen((uint512,address,bytes64) indexed record)
func (_EventEmitter *EventEmitterFilterer) FilterRecordSeen(opts *bind.FilterOpts, record []EventEmitterRecord) (*EventEmitterRecordSeenIterator, error) {

	var recordRule []any
	for _, recordItem := range record {
		recordRule = append(recordRule, recordItem)
	}

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "RecordSeen", recordRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterRecordSeenIterator{contract: _EventEmitter.contract, event: "RecordSeen", logs: logs, sub: sub}, nil
}

// WatchRecordSeen is a free log subscription operation binding the contract event 0x58adb069ff9511033daf66a044c68afaf5ee830f0543ae234db74ca220fe4c12.
//
// Hyperion: event RecordSeen((uint512,address,bytes64) indexed record)
func (_EventEmitter *EventEmitterFilterer) WatchRecordSeen(opts *bind.WatchOpts, sink chan<- *EventEmitterRecordSeen, record []EventEmitterRecord) (event.Subscription, error) {

	var recordRule []any
	for _, recordItem := range record {
		recordRule = append(recordRule, recordItem)
	}

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "RecordSeen", recordRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterRecordSeen)
				if err := _EventEmitter.contract.UnpackLog(event, "RecordSeen", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseRecordSeen is a log parse operation binding the contract event 0x58adb069ff9511033daf66a044c68afaf5ee830f0543ae234db74ca220fe4c12.
//
// Hyperion: event RecordSeen((uint512,address,bytes64) indexed record)
func (_EventEmitter *EventEmitterFilterer) ParseRecordSeen(log types.Log) (*EventEmitterRecordSeen, error) {
	event := new(EventEmitterRecordSeen)
	if err := _EventEmitter.contract.UnpackLog(event, "RecordSeen", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterStoredIterator is returned from FilterStored and is used to iterate over the raw logs and unpacked data for Stored events raised by the EventEmitter contract.
type EventEmitterStoredIterator struct {
	Event *EventEmitterStored // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log   // Log channel receiving the found contract events
	sub  qrl.Subscription // Subscription for errors, completion and termination
	done bool             // Whether the subscription completed delivering logs
	fail error            // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EventEmitterStoredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterStored)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EventEmitterStored)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EventEmitterStoredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterStoredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterStored represents a Stored event raised by the EventEmitter contract.
type EventEmitterStored struct {
	Recipient common.Address
	Amount    *big.Int
	Delta     *big.Int
	Tag       [64]byte
	Payload   []byte
	Note      string
	Enabled   bool
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterStored is a free log retrieval operation binding the contract event 0x0971a927eb69632cd5aced366c9dd3ee5626b6c0a27cb781139eeffab9e5372f.
//
// Hyperion: event Stored(address indexed recipient, uint512 indexed amount, int512 indexed delta, bytes64 tag, bytes payload, string note, bool enabled)
func (_EventEmitter *EventEmitterFilterer) FilterStored(opts *bind.FilterOpts, recipient []common.Address, amount []*big.Int, delta []*big.Int) (*EventEmitterStoredIterator, error) {

	var recipientRule []any
	for _, recipientItem := range recipient {
		recipientRule = append(recipientRule, recipientItem)
	}
	var amountRule []any
	for _, amountItem := range amount {
		amountRule = append(amountRule, amountItem)
	}
	var deltaRule []any
	for _, deltaItem := range delta {
		deltaRule = append(deltaRule, deltaItem)
	}

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "Stored", recipientRule, amountRule, deltaRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterStoredIterator{contract: _EventEmitter.contract, event: "Stored", logs: logs, sub: sub}, nil
}

// WatchStored is a free log subscription operation binding the contract event 0x0971a927eb69632cd5aced366c9dd3ee5626b6c0a27cb781139eeffab9e5372f.
//
// Hyperion: event Stored(address indexed recipient, uint512 indexed amount, int512 indexed delta, bytes64 tag, bytes payload, string note, bool enabled)
func (_EventEmitter *EventEmitterFilterer) WatchStored(opts *bind.WatchOpts, sink chan<- *EventEmitterStored, recipient []common.Address, amount []*big.Int, delta []*big.Int) (event.Subscription, error) {

	var recipientRule []any
	for _, recipientItem := range recipient {
		recipientRule = append(recipientRule, recipientItem)
	}
	var amountRule []any
	for _, amountItem := range amount {
		amountRule = append(amountRule, amountItem)
	}
	var deltaRule []any
	for _, deltaItem := range delta {
		deltaRule = append(deltaRule, deltaItem)
	}

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "Stored", recipientRule, amountRule, deltaRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterStored)
				if err := _EventEmitter.contract.UnpackLog(event, "Stored", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseStored is a log parse operation binding the contract event 0x0971a927eb69632cd5aced366c9dd3ee5626b6c0a27cb781139eeffab9e5372f.
//
// Hyperion: event Stored(address indexed recipient, uint512 indexed amount, int512 indexed delta, bytes64 tag, bytes payload, string note, bool enabled)
func (_EventEmitter *EventEmitterFilterer) ParseStored(log types.Log) (*EventEmitterStored, error) {
	event := new(EventEmitterStored)
	if err := _EventEmitter.contract.UnpackLog(event, "Stored", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterTransformedIterator is returned from FilterTransformed and is used to iterate over the raw logs and unpacked data for Transformed events raised by the EventEmitter contract.
type EventEmitterTransformedIterator struct {
	Event *EventEmitterTransformed // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log   // Log channel receiving the found contract events
	sub  qrl.Subscription // Subscription for errors, completion and termination
	done bool             // Whether the subscription completed delivering logs
	fail error            // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EventEmitterTransformedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterTransformed)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EventEmitterTransformed)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EventEmitterTransformedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterTransformedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterTransformed represents a Transformed event raised by the EventEmitter contract.
type EventEmitterTransformed struct {
	Value uint16
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterTransformed is a free log retrieval operation binding the contract event 0xe3843251954de1b1a308319c1aa57a527f6a902f9336e038c96857e4b0b82354.
//
// Hyperion: event Transformed(uint16 value)
func (_EventEmitter *EventEmitterFilterer) FilterTransformed(opts *bind.FilterOpts) (*EventEmitterTransformedIterator, error) {

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "Transformed")
	if err != nil {
		return nil, err
	}
	return &EventEmitterTransformedIterator{contract: _EventEmitter.contract, event: "Transformed", logs: logs, sub: sub}, nil
}

// WatchTransformed is a free log subscription operation binding the contract event 0xe3843251954de1b1a308319c1aa57a527f6a902f9336e038c96857e4b0b82354.
//
// Hyperion: event Transformed(uint16 value)
func (_EventEmitter *EventEmitterFilterer) WatchTransformed(opts *bind.WatchOpts, sink chan<- *EventEmitterTransformed) (event.Subscription, error) {

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "Transformed")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterTransformed)
				if err := _EventEmitter.contract.UnpackLog(event, "Transformed", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTransformed is a log parse operation binding the contract event 0xe3843251954de1b1a308319c1aa57a527f6a902f9336e038c96857e4b0b82354.
//
// Hyperion: event Transformed(uint16 value)
func (_EventEmitter *EventEmitterFilterer) ParseTransformed(log types.Log) (*EventEmitterTransformed, error) {
	event := new(EventEmitterTransformed)
	if err := _EventEmitter.contract.UnpackLog(event, "Transformed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterTransformed0Iterator is returned from FilterTransformed0 and is used to iterate over the raw logs and unpacked data for Transformed0 events raised by the EventEmitter contract.
type EventEmitterTransformed0Iterator struct {
	Event *EventEmitterTransformed0 // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log   // Log channel receiving the found contract events
	sub  qrl.Subscription // Subscription for errors, completion and termination
	done bool             // Whether the subscription completed delivering logs
	fail error            // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EventEmitterTransformed0Iterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterTransformed0)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EventEmitterTransformed0)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EventEmitterTransformed0Iterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterTransformed0Iterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterTransformed0 represents a Transformed0 event raised by the EventEmitter contract.
type EventEmitterTransformed0 struct {
	Value string
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterTransformed0 is a free log retrieval operation binding the contract event 0x53b082deb2f7988df478883fee52c7e9450a5d38daee88ec2bef0543941b46ae.
//
// Hyperion: event Transformed(string value)
func (_EventEmitter *EventEmitterFilterer) FilterTransformed0(opts *bind.FilterOpts) (*EventEmitterTransformed0Iterator, error) {

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "Transformed0")
	if err != nil {
		return nil, err
	}
	return &EventEmitterTransformed0Iterator{contract: _EventEmitter.contract, event: "Transformed0", logs: logs, sub: sub}, nil
}

// WatchTransformed0 is a free log subscription operation binding the contract event 0x53b082deb2f7988df478883fee52c7e9450a5d38daee88ec2bef0543941b46ae.
//
// Hyperion: event Transformed(string value)
func (_EventEmitter *EventEmitterFilterer) WatchTransformed0(opts *bind.WatchOpts, sink chan<- *EventEmitterTransformed0) (event.Subscription, error) {

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "Transformed0")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterTransformed0)
				if err := _EventEmitter.contract.UnpackLog(event, "Transformed0", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTransformed0 is a log parse operation binding the contract event 0x53b082deb2f7988df478883fee52c7e9450a5d38daee88ec2bef0543941b46ae.
//
// Hyperion: event Transformed(string value)
func (_EventEmitter *EventEmitterFilterer) ParseTransformed0(log types.Log) (*EventEmitterTransformed0, error) {
	event := new(EventEmitterTransformed0)
	if err := _EventEmitter.contract.UnpackLog(event, "Transformed0", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
