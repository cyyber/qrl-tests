//go:build e2e

package abi

import (
	"context"
	"errors"
	"math/big"
	"strings"

	"github.com/cyyber/qrl-tests/e2e/internal/contracts/abifixture"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/accounts/abi"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	qrlmath "github.com/theQRL/go-qrl/common/math"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/rpc"
)

func (fixture *liveFixture) callRevertData(
	ctx context.Context,
	method string,
	args ...any,
) []byte {
	ginkgo.GinkgoHelper()

	var output []any
	callErr := fixture.contract.Call(
		&bind.CallOpts{Context: ctx, BlockNumber: fixture.deploymentBlock},
		&output,
		method,
		args...,
	)
	gomega.Expect(callErr).To(gomega.HaveOccurred(), "%s unexpectedly succeeded", method)
	var dataError rpc.DataError
	gomega.Expect(errors.As(callErr, &dataError)).To(
		gomega.BeTrue(),
		"%s returned %T, want rpc.DataError",
		method,
		callErr,
	)
	encoded, ok := dataError.ErrorData().(string)
	gomega.Expect(ok).To(
		gomega.BeTrue(),
		"%s revert data has type %T, want hex string",
		method,
		dataError.ErrorData(),
	)
	revertData, err := hexutil.Decode(encoded)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "decode %s revert data %q", method, encoded)
	return revertData
}

func (fixture *liveFixture) assertErrors(ctx context.Context) {
	ginkgo.GinkgoHelper()

	inputs := fixture.inputs
	record := abifixture.EventEmitterRecord{
		Amount:    inputs.amount,
		Recipient: fixture.from,
		Tag:       inputs.tag,
	}
	complexArguments := []any{
		inputs.amount,
		"unique custom error across a VM word: " + inputs.note,
		inputs.payload,
		record,
		[][]uint16{{}, {1, 0xffff}, {0x1234}},
	}

	// Hyperion:
	// error ComplexFailure(uint512, string, bytes, Record, uint16[][]);
	// function failComplex(...) external pure {
	//     revert ComplexFailure(code, reason, payload, record, nested);
	// }
	// Goal: RPC revert data equals the four-byte selector followed by the
	// canonical VM encoding of every custom-error argument; selector lookup,
	// decoding, and re-encoding then reproduce the same payload.
	ginkgo.By("round-tripping a complex custom error through raw revert data and selector-based ABI decoding")
	definition, ok := fixture.contractABI.Errors["ComplexFailure"]
	gomega.Expect(ok).To(gomega.BeTrue(), "ABI has no ComplexFailure error")
	signature := definition.Sig
	revertData := fixture.callRevertData(ctx, "failComplex", complexArguments...)
	encodedArguments, err := definition.Inputs.Pack(complexArguments...)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "pack %s", signature)
	wantRevertData := append([]byte{}, definition.ID[:4]...)
	wantRevertData = append(wantRevertData, encodedArguments...)
	gomega.Expect(revertData).To(gomega.Equal(wantRevertData), "%s compiler revert", signature)

	var errorSelector [4]byte
	copy(errorSelector[:], revertData)
	resolvedError, err := fixture.contractABI.ErrorByID(errorSelector)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "ErrorByID(%s)", signature)
	gomega.Expect(resolvedError.Sig).To(gomega.Equal(signature))
	decoded, err := resolvedError.Unpack(revertData)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "decode %s", signature)
	decodedArguments, ok := decoded.([]any)
	gomega.Expect(ok).To(gomega.BeTrue(), "decoded %s has type %T", signature, decoded)
	repackedArguments, err := resolvedError.Inputs.Pack(decodedArguments...)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "repack %s", signature)
	gomega.Expect(repackedArguments).To(
		gomega.Equal(encodedArguments),
		"decoded %s did not round-trip",
		signature,
	)

	// Hyperion:
	// function failReason() external pure { revert("VM standard revert reason"); }
	// function failPanic() external pure { assert(false); }
	// Goal: standard Error(string) and Panic(uint256) payloads decode to their
	// human-readable reason.
	ginkgo.By("decoding standard Error(string) and Panic(uint256) revert payloads")
	for _, standardError := range []struct {
		method string
		want   string
	}{
		{method: "failReason", want: "VM standard revert reason"},
		{method: "failPanic", want: "assert(false)"},
	} {
		reason, err := abi.UnpackRevert(fixture.callRevertData(ctx, standardError.method))
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "decode %s", standardError.method)
		gomega.Expect(reason).To(gomega.Equal(standardError.want), standardError.method)
	}

	// Hyperion:
	// function failReason() external pure { revert("VM standard revert reason"); }
	// Goal: submitting the reverting call as a transaction produces a mined
	// receipt whose status is explicitly failed.
	ginkgo.By("requiring a failed receipt for a reverting transaction")
	auth := fixture.transactOpts(ctx)
	auth.GasLimit = 1_000_000
	failedTx, err := fixture.contract.Transact(auth, "failReason")
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "submit reverting transaction")
	failedReceipt := fixture.waitTransaction(ctx, failedTx)
	gomega.Expect(failedReceipt.Status).To(
		gomega.Equal(types.ReceiptStatusFailed),
		"reverting transaction %s status",
		failedTx.Hash(),
	)
}

func (fixture *liveFixture) assertPayableEntrypoints(ctx context.Context) {
	ginkgo.GinkgoHelper()

	// Hyperion:
	// event Received(uint256 amount);
	// receive() external payable { emit Received(msg.value); }
	// Goal: the generated receive entrypoint sends the requested value with
	// empty calldata, and its receipt plus generated parser reproduce that value.
	ginkgo.By("sending value through the generated receive entrypoint")
	amount := big.NewInt(11)
	auth := fixture.transactOpts(ctx)
	auth.Value = amount
	auth.GasLimit = 1_000_000
	tx, err := fixture.binding.Receive(auth)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "generated receive transaction")
	gomega.Expect(tx.To()).NotTo(gomega.BeNil())
	gomega.Expect(*tx.To()).To(gomega.Equal(fixture.address))
	gomega.Expect(tx.Data()).To(gomega.BeEmpty())
	gomega.Expect(tx.Value()).To(gomega.Equal(amount))
	receipt := fixture.waitSuccessfulTransaction(ctx, tx)
	gomega.Expect(receipt.Logs).To(gomega.HaveLen(1))
	fixture.assertEvent(ctx, eventExpectation{
		name: "Received",
		log:  *receipt.Logs[0],
		data: []any{amount},
		exactTopics: []common.LogTopic{
			common.HashToLogTopic(fixture.contractABI.Events["Received"].ID),
		},
		want: map[string]any{"amount": amount},
	})
	received, err := fixture.binding.ParseReceived(*receipt.Logs[0])
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(received.Amount).To(gomega.Equal(amount))

	// Hyperion:
	// event FallbackCalled(bytes payload, uint256 amount);
	// fallback() external payable { emit FallbackCalled(msg.data, msg.value); }
	// Goal: the generated fallback entrypoint preserves calldata larger than
	// one VM word and the transferred value in both the transaction and log.
	ginkgo.By("sending calldata and value through the generated fallback entrypoint")
	payload := []byte(strings.Repeat("\x5a", 65))
	amount = big.NewInt(13)
	auth = fixture.transactOpts(ctx)
	auth.Value = amount
	auth.GasLimit = 1_000_000
	tx, err = fixture.binding.Fallback(auth, payload)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "generated fallback transaction")
	gomega.Expect(tx.To()).NotTo(gomega.BeNil())
	gomega.Expect(*tx.To()).To(gomega.Equal(fixture.address))
	gomega.Expect(tx.Data()).To(gomega.Equal(payload))
	gomega.Expect(tx.Value()).To(gomega.Equal(amount))
	receipt = fixture.waitSuccessfulTransaction(ctx, tx)
	gomega.Expect(receipt.Logs).To(gomega.HaveLen(1))
	fixture.assertEvent(ctx, eventExpectation{
		name: "FallbackCalled",
		log:  *receipt.Logs[0],
		data: []any{payload, amount},
		exactTopics: []common.LogTopic{
			common.HashToLogTopic(fixture.contractABI.Events["FallbackCalled"].ID),
		},
		want: map[string]any{
			"payload": payload,
			"amount":  amount,
		},
	})
	fallback, err := fixture.binding.ParseFallbackCalled(*receipt.Logs[0])
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(fallback.Payload).To(gomega.Equal(payload))
	gomega.Expect(fallback.Amount).To(gomega.Equal(amount))

	// Hyperion:
	// event Paid(address indexed sender, uint16 indexed marker, uint256 amount);
	// function pay(uint16 marker) external payable {
	//     emit Paid(msg.sender, marker, msg.value);
	// }
	// Goal: a named generated payable method preserves its argument and value,
	// and its indexed event can be decoded and positively or negatively filtered.
	ginkgo.By("sending value through a named generated payable method")
	const marker = uint16(0xbabe)
	amount = big.NewInt(17)
	auth = fixture.transactOpts(ctx)
	auth.Value = amount
	payTx, err := fixture.binding.Pay(auth, marker)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "generated named payable transaction")
	payReceipt := fixture.waitSuccessfulTransaction(ctx, payTx)
	gomega.Expect(payReceipt.Logs).To(gomega.HaveLen(1))
	fixture.assertEvent(ctx, eventExpectation{
		name: "Paid",
		log:  *payReceipt.Logs[0],
		data: []any{amount},
		exactTopics: []common.LogTopic{
			common.HashToLogTopic(fixture.contractABI.Events["Paid"].ID),
			common.BytesToLeftAlignedLogTopic(fixture.from[:]),
			common.BytesToRightAlignedLogTopic(qrlmath.U512Bytes(new(big.Int).SetUint64(uint64(marker)))),
		},
		want: map[string]any{
			"sender": fixture.from,
			"marker": marker,
			"amount": amount,
		},
		filter: [][]any{{fixture.from}, {marker}},
		reject: [][]any{{fixture.from}, {marker + 1}},
	})
	paid, err := fixture.binding.ParsePaid(*payReceipt.Logs[0])
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(paid.Sender).To(gomega.Equal(fixture.from))
	gomega.Expect(paid.Marker).To(gomega.Equal(marker))
	gomega.Expect(paid.Amount).To(gomega.Equal(amount))
}
