// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

//go:build e2e

package abi

import (
	"context"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

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
