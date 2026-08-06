//go:build e2e

package abi

import (
	"context"
	"math/big"
	"strings"

	"github.com/cyyber/qrl-tests/e2e/internal/abifixture"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	qrl "github.com/theQRL/go-qrl"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
)

// assertCall checks BoundContract decoding and independently proves that
// the compiler returned the canonical ABI bytes.
func (fixture *liveFixture) assertCall(
	ctx context.Context,
	method string,
	args, want []any,
) {
	ginkgo.GinkgoHelper()

	var decoded []any
	err := fixture.contract.Call(
		&bind.CallOpts{Context: ctx, BlockNumber: fixture.deploymentBlock},
		&decoded,
		method,
		args...,
	)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "%s through BoundContract", method)
	wantOutput, err := fixture.contractABI.Methods[method].Outputs.Pack(want...)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "pack canonical %s output", method)
	repacked, err := fixture.contractABI.Methods[method].Outputs.Pack(decoded...)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "repack BoundContract %s output", method)
	gomega.Expect(repacked).To(gomega.Equal(wantOutput), "BoundContract %s output", method)
	input, err := fixture.contractABI.Pack(method, args...)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "pack %s input", method)
	raw, err := fixture.client.CallContract(
		ctx,
		qrl.CallMsg{From: fixture.from, To: &fixture.address, Data: input},
		fixture.deploymentBlock,
	)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "raw %s call", method)
	gomega.Expect(raw).To(gomega.Equal(wantOutput), "compiler %s output", method)
}

func (fixture *liveFixture) assertCallRoundTrips(ctx context.Context) {
	ginkgo.GinkgoHelper()

	inputs := fixture.inputs

	// Hyperion:
	// function echo(uint512, int512, bytes64, address, bytes, string, bool)
	//     external pure returns (uint512, int512, bytes64, address, bytes, string, bool);
	// Goal: generic ABI decoding and raw RPC return exactly the values sent.
	ginkgo.By("round-tripping scalar and dynamic values through generic ABI and raw RPC")
	echoValues := []any{
		inputs.amount,
		inputs.delta,
		inputs.tag,
		fixture.from,
		inputs.payload,
		inputs.note,
		true,
	}
	fixture.assertCall(ctx, "echo", echoValues, echoValues)

	callOpts := fixture.callOpts(ctx)

	// Hyperion:
	// function echoNested(DynamicRecord, DynamicRecord[], uint16[][][])
	//     external pure returns (DynamicRecord, DynamicRecord[], uint16[][][]);
	// Goal: generated bindings preserve nested tuples, arrays, and empty values.
	ginkgo.By("round-tripping nested tuples and arrays through generated bindings")
	nested := abifixture.EventEmitterDynamicRecord{
		Amount:  inputs.amount,
		Note:    inputs.note,
		Payload: inputs.payload,
		Values:  [][]uint16{{1, 2}, {}, {3}},
	}
	records := []abifixture.EventEmitterDynamicRecord{
		nested,
		{Amount: new(big.Int), Note: "", Payload: []byte{}, Values: [][]uint16{}},
	}
	cube := [][][]uint16{{{1}, {2, 3}}, {}, {{4}}}
	gotNested, gotRecords, gotCube, err := fixture.binding.EchoNested(
		callOpts,
		nested,
		records,
		cube,
	)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	outputs := fixture.contractABI.Methods["echoNested"].Outputs
	want, err := outputs.Pack(nested, records, cube)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "pack canonical echoNested output")
	got, err := outputs.Pack(gotNested, gotRecords, gotCube)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "repack generated echoNested output")
	gomega.Expect(got).To(gomega.Equal(want), "generated echoNested output")
	nestedValues := []any{nested, records, cube}
	fixture.assertCall(ctx, "echoNested", nestedValues, nestedValues)

	// Hyperion: function observe() external view returns (uint512 value, address caller);
	// Goal: the generated view returns the constructor state and original caller.
	ginkgo.By("reading constructor state through a generated view")
	observed, err := fixture.binding.Observe(callOpts)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(observed.Value).To(gomega.Equal(fixture.initial))
	gomega.Expect(observed.Caller).To(gomega.Equal(fixture.from))

	// Hyperion:
	// function transform(string) external pure returns (string);
	// function transform(uint16) external pure returns (uint16);
	// Goal: ABI lookup and generated wrappers select the correct overload.
	ginkgo.By("resolving overloaded methods through their generated wrappers")
	stringMethod := fixture.contractABI.Methods["transform"]
	integerMethod := fixture.contractABI.Methods["transform0"]
	gomega.Expect(stringMethod.Sig).To(gomega.Equal("transform(string)"))
	gomega.Expect(integerMethod.Sig).To(gomega.Equal("transform(uint16)"))
	transformedString, err := fixture.binding.Transform(callOpts, inputs.note)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(transformedString).To(gomega.Equal(inputs.note))
	transformedInteger, err := fixture.binding.Transform0(callOpts, 0xfffe)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(transformedInteger).To(gomega.Equal(uint16(0xffff)))
}

func (fixture *liveFixture) assertDynamicContainers(ctx context.Context) {
	ginkgo.GinkgoHelper()

	// Hyperion:
	// function echoDynamicContainers(bytes[2], bytes[], string[])
	//     external pure returns (...);
	// Goal: fixed and dynamic containers whose elements have dynamic tails
	// preserve empty and multi-word members.
	ginkgo.By("round-tripping fixed and dynamic containers with dynamic elements")
	fixedDynamicBytes := [2][]byte{{}, patternedBytes(65, 0x61)}
	byteSlices := [][]byte{
		{}, patternedBytes(1, 0x71), patternedBytes(65, 0x72), patternedBytes(129, 0x73),
	}
	stringValues := []string{"", "x", strings.Repeat("s", 65), strings.Repeat("t", 129)}
	values := []any{fixedDynamicBytes, byteSlices, stringValues}
	fixture.assertCall(ctx, "echoDynamicContainers", values, values)
	gotFixedDynamicBytes, gotByteSlices, gotStrings, err :=
		fixture.binding.EchoDynamicContainers(
			fixture.callOpts(ctx), fixedDynamicBytes, byteSlices, stringValues,
		)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(gotFixedDynamicBytes).To(gomega.Equal(fixedDynamicBytes))
	gomega.Expect(gotByteSlices).To(gomega.Equal(byteSlices))
	gomega.Expect(gotStrings).To(gomega.Equal(stringValues))
}

func (fixture *liveFixture) assertLeafContainers(ctx context.Context) {
	ginkgo.GinkgoHelper()

	// Hyperion:
	// function echoLeafContainers(address[2], address[], bytes64[2], bytes64[])
	//     external pure returns (...);
	// Goal: fixed and dynamic containers of full-word leaves preserve zero and
	// distinct nonzero 64-byte addresses and bytes64 values.
	ginkgo.By("round-tripping 64-byte address and fixed-bytes containers")
	secondAddress := fixture.from
	secondAddress[0] ^= 0x80
	secondAddress[len(secondAddress)-1] ^= 0x01
	secondTag := fixture.inputs.tag
	secondTag[0] ^= 0xff
	secondTag[len(secondTag)-1] ^= 0xff
	fixedAddresses := [2]common.Address{{}, fixture.from}
	addresses := []common.Address{secondAddress, {}, fixture.from}
	fixedTags := [2][64]byte{{}, fixture.inputs.tag}
	tags := [][64]byte{secondTag, {}, fixture.inputs.tag}
	values := []any{fixedAddresses, addresses, fixedTags, tags}
	fixture.assertCall(ctx, "echoLeafContainers", values, values)
	gotFixedAddresses, gotAddresses, gotFixedTags, gotTags, err :=
		fixture.binding.EchoLeafContainers(
			fixture.callOpts(ctx), fixedAddresses, addresses, fixedTags, tags,
		)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(gotFixedAddresses).To(gomega.Equal(fixedAddresses))
	gomega.Expect(gotAddresses).To(gomega.Equal(addresses))
	gomega.Expect(gotFixedTags).To(gomega.Equal(fixedTags))
	gomega.Expect(gotTags).To(gomega.Equal(tags))
}

func (fixture *liveFixture) assertNestedComposites(ctx context.Context) {
	ginkgo.GinkgoHelper()

	secondAddress := fixture.from
	secondAddress[0] ^= 0x80
	secondAddress[len(secondAddress)-1] ^= 0x01
	secondTag := fixture.inputs.tag
	secondTag[0] ^= 0xff
	secondTag[len(secondTag)-1] ^= 0xff

	// Hyperion:
	// function echoCompositeContainers(uint16[2][2], uint16[2][], DynamicRecord[2], NestedRecord)
	//     external pure returns (...);
	// Goal: fixed-of-fixed arrays, dynamic arrays of fixed arrays, fixed arrays
	// of dynamic tuples, and tuples nested inside tuples retain their shape.
	ginkgo.By("round-tripping offset-heavy arrays and nested tuples")
	fixedMatrix := [2][2]uint16{{0, 0xffff}, {1, 0x1234}}
	rows := [][2]uint16{{}, {1, 0xffff}, {0x1234, 0x4321}}
	records := [2]abifixture.EventEmitterDynamicRecord{
		{
			Amount: fixture.inputs.amount, Note: fixture.inputs.note,
			Payload: fixture.inputs.payload, Values: [][]uint16{{1, 2}, {}, {3}},
		},
		{Amount: new(big.Int), Note: "", Payload: []byte{}, Values: [][]uint16{}},
	}
	nested := abifixture.EventEmitterNestedRecord{
		FixedRecord: abifixture.EventEmitterRecord{
			Amount: fixture.inputs.amount, Recipient: secondAddress, Tag: secondTag,
		},
		DynamicRecord: records[0],
		Extra:         patternedBytes(65, 0x91),
	}
	values := []any{fixedMatrix, rows, records, nested}
	fixture.assertCall(ctx, "echoCompositeContainers", values, values)
	gotFixedMatrix, gotRows, gotRecords, gotNested, err :=
		fixture.binding.EchoCompositeContainers(
			fixture.callOpts(ctx), fixedMatrix, rows, records, nested,
		)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(gotFixedMatrix).To(gomega.Equal(fixedMatrix))
	gomega.Expect(gotRows).To(gomega.Equal(rows))
	for index := range records {
		assertDynamicRecordEqual(gotRecords[index], records[index], "dynamic record")
	}
	gomega.Expect(gotNested.FixedRecord.Amount.Cmp(nested.FixedRecord.Amount)).To(gomega.Equal(0))
	gomega.Expect(gotNested.FixedRecord.Recipient).To(gomega.Equal(nested.FixedRecord.Recipient))
	gomega.Expect(gotNested.FixedRecord.Tag).To(gomega.Equal(nested.FixedRecord.Tag))
	assertDynamicRecordEqual(gotNested.DynamicRecord, nested.DynamicRecord, "nested record")
	gomega.Expect(gotNested.Extra).To(gomega.Equal(nested.Extra))
}

func assertDynamicRecordEqual(got, want abifixture.EventEmitterDynamicRecord, context string) {
	ginkgo.GinkgoHelper()

	gomega.Expect(got.Amount.Cmp(want.Amount)).To(gomega.Equal(0), context)
	gomega.Expect(got.Note).To(gomega.Equal(want.Note), context)
	gomega.Expect(got.Payload).To(gomega.Equal(want.Payload), context)
	gomega.Expect(got.Values).To(gomega.Equal(want.Values), context)
}
