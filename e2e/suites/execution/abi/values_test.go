//go:build e2e

package abi

import (
	"context"
	"math/big"
	"strings"

	"github.com/cyyber/qrl-tests/e2e/internal/abifixture"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

type integerEdgeCase struct {
	name  string
	edges abifixture.EventEmitterBoundaryEdges
}

func integerEdgeCases() []integerEdgeCase {
	return []integerEdgeCase{
		{name: "zero", edges: zeroBoundaryEdges()},
		{
			name: "unsigned maxima and signed minima",
			edges: abifixture.EventEmitterBoundaryEdges{
				Unsigned248: unsignedMaximum(248), Signed248: signedMinimum(248),
				Unsigned256: unsignedMaximum(256), Signed256: signedMinimum(256),
				Unsigned264: unsignedMaximum(264), Signed264: signedMinimum(264),
				Unsigned504: unsignedMaximum(504), Signed504: signedMinimum(504),
				Unsigned512: unsignedMaximum(512), Signed512: signedMinimum(512),
			},
		},
		{
			name: "signed maxima",
			edges: abifixture.EventEmitterBoundaryEdges{
				Unsigned248: big.NewInt(1), Signed248: signedMaximum(248),
				Unsigned256: big.NewInt(1), Signed256: signedMaximum(256),
				Unsigned264: big.NewInt(1), Signed264: signedMaximum(264),
				Unsigned504: big.NewInt(1), Signed504: signedMaximum(504),
				Unsigned512: big.NewInt(1), Signed512: signedMaximum(512),
			},
		},
		{
			name: "negative one",
			edges: abifixture.EventEmitterBoundaryEdges{
				Unsigned248: big.NewInt(1), Signed248: big.NewInt(-1),
				Unsigned256: big.NewInt(1), Signed256: big.NewInt(-1),
				Unsigned264: big.NewInt(1), Signed264: big.NewInt(-1),
				Unsigned504: big.NewInt(1), Signed504: big.NewInt(-1),
				Unsigned512: big.NewInt(1), Signed512: big.NewInt(-1),
			},
		},
	}
}

func unsignedMaximum(bits uint) *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits), big.NewInt(1))
}

func zeroBoundaryEdges() abifixture.EventEmitterBoundaryEdges {
	return abifixture.EventEmitterBoundaryEdges{
		Unsigned248: new(big.Int), Signed248: new(big.Int),
		Unsigned256: new(big.Int), Signed256: new(big.Int),
		Unsigned264: new(big.Int), Signed264: new(big.Int),
		Unsigned504: new(big.Int), Signed504: new(big.Int),
		Unsigned512: new(big.Int), Signed512: new(big.Int),
	}
}

func signedMinimum(bits uint) *big.Int {
	return new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), bits-1))
}

func signedMaximum(bits uint) *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits-1), big.NewInt(1))
}

func assertBoundaryEdgesEqual(got, want abifixture.EventEmitterBoundaryEdges, context string) {
	ginkgo.GinkgoHelper()

	for _, values := range [][2]*big.Int{
		{got.Unsigned248, want.Unsigned248}, {got.Signed248, want.Signed248},
		{got.Unsigned256, want.Unsigned256}, {got.Signed256, want.Signed256},
		{got.Unsigned264, want.Unsigned264}, {got.Signed264, want.Signed264},
		{got.Unsigned504, want.Unsigned504}, {got.Signed504, want.Signed504},
		{got.Unsigned512, want.Unsigned512}, {got.Signed512, want.Signed512},
	} {
		gomega.Expect(values[0].Cmp(values[1])).To(gomega.Equal(0), context)
	}
	gomega.Expect(got.Bytes31Value).To(gomega.Equal(want.Bytes31Value), context)
	gomega.Expect(got.Bytes32Value).To(gomega.Equal(want.Bytes32Value), context)
	gomega.Expect(got.Bytes33Value).To(gomega.Equal(want.Bytes33Value), context)
	gomega.Expect(got.Bytes63Value).To(gomega.Equal(want.Bytes63Value), context)
	gomega.Expect(got.Bytes64Value).To(gomega.Equal(want.Bytes64Value), context)
}

func fillPattern(destination []byte, seed byte) {
	for index := range destination {
		destination[index] = seed + byte(index*29)
	}
}

func patternedBytes(length int, seed byte) []byte {
	value := make([]byte, length)
	fillPattern(value, seed)
	return value
}

func (fixture *liveFixture) assertDynamicPayloadBoundaries(ctx context.Context) {
	ginkgo.GinkgoHelper()

	callOpts := fixture.callOpts(ctx)
	// Hyperion:
	// function echo(uint512, int512, bytes64, address, bytes, string, bool)
	//     external pure returns (...);
	// Goal: dynamic lengths immediately around each 64-byte boundary preserve
	// their offsets, length words, payload bytes, and padding.
	ginkgo.By("round-tripping dynamic bytes and strings around VM word boundaries")
	for _, length := range []int{0, 1, 63, 64, 65, 127, 128, 129} {
		payload := make([]byte, length)
		fillPattern(payload, byte(length))
		note := strings.Repeat(string(rune('a'+length%26)), length)
		values := []any{
			fixture.inputs.amount, fixture.inputs.delta, fixture.inputs.tag,
			fixture.from, payload, note, true,
		}
		fixture.assertCall(ctx, "echo", values, values)
		gotAmount, gotDelta, gotTag, gotRecipient, gotPayload, gotNote, gotEnabled, err :=
			fixture.binding.Echo(
				callOpts, fixture.inputs.amount, fixture.inputs.delta, fixture.inputs.tag,
				fixture.from, payload, note, true,
			)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "length %d", length)
		gomega.Expect([]any{
			gotAmount, gotDelta, gotTag, gotRecipient, gotPayload, gotNote, gotEnabled,
		}).To(gomega.Equal(values), "length %d", length)
	}
}

func (fixture *liveFixture) assertFixedBytesBoundaries(ctx context.Context) {
	ginkgo.GinkgoHelper()

	// Hyperion:
	// function echoBoundaryEdges(BoundaryEdges edges)
	//     external pure returns (BoundaryEdges);
	// Goal: fixed bytes remain left-aligned across the legacy 32-byte boundary
	// and at the 64-byte VM word boundary.
	ginkgo.By("round-tripping fixed bytes across ABI word boundaries")
	fixedBytes := zeroBoundaryEdges()
	fillPattern(fixedBytes.Bytes31Value[:], 0x11)
	fillPattern(fixedBytes.Bytes32Value[:], 0x22)
	fillPattern(fixedBytes.Bytes33Value[:], 0x33)
	fillPattern(fixedBytes.Bytes63Value[:], 0x44)
	fillPattern(fixedBytes.Bytes64Value[:], 0x55)
	fixture.assertCall(ctx, "echoBoundaryEdges", []any{fixedBytes}, []any{fixedBytes})
	got, err := fixture.binding.EchoBoundaryEdges(fixture.callOpts(ctx), fixedBytes)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	assertBoundaryEdgesEqual(got, fixedBytes, "fixed bytes")
}

func (fixture *liveFixture) assertMixedBoundaries(ctx context.Context) {
	ginkgo.GinkgoHelper()

	callOpts := fixture.callOpts(ctx)
	inputs := fixture.inputs

	// Hyperion:
	// function echoBoundaries(uint8, int8, uint256, int256, bytes5, uint16[3], string[2], uint16[][2])
	//     external pure returns (uint8, int8, uint256, int256, bytes5, uint16[3], string[2], uint16[][2]);
	// Goal: generated bindings preserve fixed/dynamic values plus uint256 zero-extension
	// and int256 sign-extension in 64-byte ABI words.
	ginkgo.By("round-tripping integer widths and fixed/dynamic boundary values through generated bindings")
	wideUnsigned := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(0x1234))
	wideSigned := new(big.Int).Add(new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 254)), big.NewInt(42))
	shortBytes := [5]byte{0x00, 0x7f, 0x80, 0xfe, 0xff}
	fixedNumbers := [3]uint16{0, 0xffff, 0x1234}
	fixedStrings := [2]string{"", inputs.note}
	mixed := [2][]uint16{{}, {1, 0xffff, 0x1234}}
	smallUnsigned, smallSigned := uint8(0xff), int8(-128)
	gotUnsigned, gotSigned, gotWideUnsigned, gotWideSigned,
		gotShortBytes, gotFixedNumbers, gotFixedStrings, gotMixed, err :=
		fixture.binding.EchoBoundaries(
			callOpts, smallUnsigned, smallSigned, wideUnsigned, wideSigned,
			shortBytes, fixedNumbers, fixedStrings, mixed,
		)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	want := []any{
		smallUnsigned, smallSigned, wideUnsigned, wideSigned,
		shortBytes, fixedNumbers, fixedStrings, mixed,
	}
	gomega.Expect([]any{
		gotUnsigned, gotSigned, gotWideUnsigned, gotWideSigned,
		gotShortBytes, gotFixedNumbers, gotFixedStrings, gotMixed,
	}).To(gomega.Equal(want))
	fixture.assertCall(ctx, "echoBoundaries", want, want)
}

func (fixture *liveFixture) assertIntegerEdge(ctx context.Context, test integerEdgeCase) {
	ginkgo.GinkgoHelper()

	fixture.assertCall(ctx, "echoBoundaryEdges", []any{test.edges}, []any{test.edges})
	got, err := fixture.binding.EchoBoundaryEdges(fixture.callOpts(ctx), test.edges)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), test.name)
	assertBoundaryEdgesEqual(got, test.edges, test.name)
}
