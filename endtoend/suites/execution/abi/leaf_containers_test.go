// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

//go:build e2e

package abi

import (
	"context"

	"github.com/theQRL/go-qrl/common"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

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
