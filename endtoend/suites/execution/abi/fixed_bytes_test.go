// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

//go:build e2e

package abi

import (
	"context"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

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
