// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

//go:build e2e

package abi

import (
	"math/big"

	"github.com/cyyber/qrl-tests/endtoend/internal/contracts/abifixture"
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
