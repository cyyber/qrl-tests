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
