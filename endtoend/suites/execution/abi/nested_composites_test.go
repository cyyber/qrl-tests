// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

//go:build e2e

package abi

import (
	"context"
	"math/big"

	"github.com/cyyber/qrl-tests/endtoend/internal/contracts/abifixture"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

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
