//go:build e2e

package abi

import (
	"context"
	"math/big"

	"github.com/cyyber/qrl-tests/e2e/internal/abifixture"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/crypto"
)

// functionValue is the ABI encoding of an external function: a 64-byte
// address followed by the 4-byte selector.
type functionValue = [common.AddressLength + 4]byte

func makeFunctionValue(address common.Address, selector []byte) functionValue {
	var value functionValue
	copy(value[:common.AddressLength], address[:])
	copy(value[common.AddressLength:], selector)
	return value
}

func (fixture *liveFixture) assertFunctionValues(ctx context.Context) {
	ginkgo.GinkgoHelper()

	// Hyperion:
	// function echoFunctions(
	//     function(uint512) external pure returns (uint512) callback,
	//     string note,
	//     function(uint512) external pure returns (uint512)[2] fixedCallbacks,
	//     function(uint512) external pure returns (uint512)[] callbacks,
	//     FunctionRecord record
	// ) external pure returns (...);
	// Goal: standalone 68-byte function values and function values nested in a
	// fixed array, dynamic array, and tuple return exactly the values sent.
	ginkgo.By("round-tripping function values and their containers through generic ABI and raw RPC")
	callback := makeFunctionValue(
		fixture.address,
		fixture.contractABI.Methods["plusOne"].ID,
	)
	secondCallback := callback
	secondCallback[len(secondCallback)-1] ^= 0xff
	fixedCallbacks := [2]functionValue{callback, secondCallback}
	callbacks := []functionValue{secondCallback, callback}
	functionRecord := abifixture.EventEmitterFunctionRecord{
		Callback: callback,
		Note:     fixture.inputs.note,
	}
	functionValues := []any{
		callback,
		fixture.inputs.note,
		fixedCallbacks,
		callbacks,
		functionRecord,
	}
	fixture.assertCall(ctx, "echoFunctions", functionValues, functionValues)

	// Hyperion:
	// function echoFunctions(...) external pure returns (...);
	// Goal: abigen represents every function value as [68]byte, including
	// values inside fixed arrays, dynamic arrays, and generated tuple types.
	ginkgo.By("round-tripping function values through generated binding types")
	callOpts := fixture.callOpts(ctx)
	gotCallback, gotNote, gotFixedCallbacks, gotCallbacks, gotRecord, err :=
		fixture.binding.EchoFunctions(
			callOpts,
			callback,
			fixture.inputs.note,
			fixedCallbacks,
			callbacks,
			functionRecord,
		)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(gotCallback).To(gomega.Equal(callback))
	gomega.Expect(gotNote).To(gomega.Equal(fixture.inputs.note))
	gomega.Expect(gotFixedCallbacks).To(gomega.Equal(fixedCallbacks))
	gomega.Expect(gotCallbacks).To(gomega.Equal(callbacks))
	gomega.Expect(gotRecord).To(gomega.Equal(functionRecord))

	// Hyperion:
	// function exerciseFunction(
	//     function(uint512) external pure returns (uint512) callback,
	//     uint512 value
	// ) external returns (function(uint512) external pure returns (uint512), uint512);
	// Goal: a decoded 68-byte function value calls the encoded contract and
	// selector, then returns the same callback and the callback result.
	ginkgo.By("executing a function value through generic ABI and raw RPC")
	functionInput := new(big.Int).Add(
		new(big.Int).Lsh(big.NewInt(1), 500),
		big.NewInt(42),
	)
	functionResult := new(big.Int).Add(functionInput, big.NewInt(1))
	fixture.assertCall(
		ctx,
		"exerciseFunction",
		[]any{callback, functionInput},
		[]any{callback, functionResult},
	)

	// Hyperion:
	// event FunctionObserved(
	//     function(uint512) external pure returns (uint512) indexed indexedCallback,
	//     function(uint512) external pure returns (uint512) callback,
	//     uint512 result
	// );
	// function exerciseFunction(...) external {
	//     uint512 result = callback(value);
	//     emit FunctionObserved(callback, callback, result);
	// }
	// Goal: generated transactions execute the callback, indexed function
	// values hash to the expected topic, and generated parsing plus filtering
	// recover the callback and result.
	ginkgo.By("executing and filtering a function value through generated bindings")
	auth := fixture.transactOpts(ctx)
	functionTx, err := fixture.binding.ExerciseFunction(auth, callback, functionInput)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "generated ExerciseFunction transaction")
	receipt := fixture.waitSuccessfulTransaction(ctx, functionTx)
	gomega.Expect(receipt.Logs).To(gomega.HaveLen(1))
	callbackHash := crypto.Keccak256Hash(callback[:])
	fixture.assertEvent(
		ctx,
		eventExpectation{
			name: "FunctionObserved",
			log:  *receipt.Logs[0],
			data: []any{callback, functionResult},
			exactTopics: []common.LogTopic{
				common.HashToLogTopic(fixture.contractABI.Events["FunctionObserved"].ID),
				common.HashToLogTopic(callbackHash),
			},
			want: map[string]any{
				"indexedCallback": callbackHash,
				"callback":        callback,
				"result":          functionResult,
			},
			filter: [][]any{{callback}},
		},
	)

	parsedEvent := mustSucceed(fixture.binding.ParseFunctionObserved(*receipt.Logs[0]))
	gomega.Expect(parsedEvent.IndexedCallback).To(gomega.Equal(callbackHash))
	gomega.Expect(parsedEvent.Callback).To(gomega.Equal(callback))
	gomega.Expect(parsedEvent.Result).To(gomega.Equal(functionResult))

	block := receipt.BlockNumber.Uint64()
	iterator := mustSucceed(fixture.binding.FilterFunctionObserved(
		&bind.FilterOpts{Start: block, End: &block, Context: ctx},
		[]functionValue{callback},
	))
	defer iterator.Close()
	gomega.Expect(iterator.Next()).To(gomega.BeTrue())
	gomega.Expect(iterator.Event.Raw.TxHash).To(gomega.Equal(receipt.TxHash))
	gomega.Expect(iterator.Next()).To(gomega.BeFalse())
	gomega.Expect(iterator.Error()).NotTo(gomega.HaveOccurred())
}
