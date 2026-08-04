// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

//go:build e2e

package abi

import (
	"context"
	"math/big"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

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
