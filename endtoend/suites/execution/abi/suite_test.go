//go:build e2e

package abi

import (
	"testing"

	"github.com/cyyber/qrl-tests/endtoend/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "ABI E2E suite")
}

var suite *liveSuite

var _ = ginkgo.BeforeSuite(func(ctx ginkgo.SpecContext) {
	suite = setupLiveSuite(ctx)
})

var _ = ginkgo.Describe(
	"ABI against a live qrl-package network",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.ContinueOnFailure,
	ginkgo.Label("e2e", "abi"),
	func() {
		// Each spec creates fresh transaction options. They share one deployment,
		// and no spec depends on state written by an earlier spec.
		var fixture *liveFixture

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			fixture = suite.deployEventEmitter(ctx)
		})

		ginkgo.It("round-trips scalar and nested ABI values through generic ABI, generated bindings, and raw RPC", func(ctx ginkgo.SpecContext) {
			fixture.assertCallRoundTrips(ctx)
		})

		ginkgo.It("round-trips mixed integer and container boundaries", func(ctx ginkgo.SpecContext) {
			fixture.assertMixedBoundaries(ctx)
		})

		for _, test := range integerEdgeCases() {
			ginkgo.It("round-trips integer edges: "+test.name, func(ctx ginkgo.SpecContext) {
				fixture.assertIntegerEdge(ctx, test)
			})
		}

		ginkgo.It("round-trips fixed bytes across ABI word boundaries", func(ctx ginkgo.SpecContext) {
			fixture.assertFixedBytesBoundaries(ctx)
		})

		ginkgo.It("round-trips dynamic payloads around VM word boundaries", func(ctx ginkgo.SpecContext) {
			fixture.assertDynamicPayloadBoundaries(ctx)
		})

		ginkgo.It("round-trips full-word leaf containers", func(ctx ginkgo.SpecContext) {
			fixture.assertLeafContainers(ctx)
		})

		ginkgo.It("round-trips containers with dynamic elements", func(ctx ginkgo.SpecContext) {
			fixture.assertDynamicContainers(ctx)
		})

		ginkgo.It("round-trips nested composite values", func(ctx ginkgo.SpecContext) {
			fixture.assertNestedComposites(ctx)
		})

		ginkgo.It("decodes custom and standard errors and requires a failed receipt", func(ctx ginkgo.SpecContext) {
			fixture.assertErrors(ctx)
		})

		ginkgo.It("round-trips Stored event topics, data, and generated and raw filters", func(ctx ginkgo.SpecContext) {
			fixture.assertStoredEventAndFilters(ctx)
		})

		ginkgo.It("hashes and filters indexed dynamic event values", func(ctx ginkgo.SpecContext) {
			fixture.assertDynamicEventAndFilters(ctx)
		})

		ginkgo.It("round-trips composite event data", func(ctx ginkgo.SpecContext) {
			fixture.assertCompositeEvent(ctx)
		})

		ginkgo.It("encodes and filters indexed scalar event values", func(ctx ginkgo.SpecContext) {
			fixture.assertIndexedScalarEvent(ctx)
		})

		ginkgo.It("resolves and decodes overloaded events", func(ctx ginkgo.SpecContext) {
			fixture.assertOverloadedEvents(ctx)
		})

		ginkgo.It("round-trips and executes function values and containers through generic ABI, generated bindings, raw RPC, events, and filters", func(ctx ginkgo.SpecContext) {
			fixture.assertFunctionValues(ctx)
		})

		ginkgo.It("executes named payable, receive, and fallback entrypoints with value, calldata, events, and filters", func(ctx ginkgo.SpecContext) {
			fixture.assertPayableEntrypoints(ctx)
		})

		ginkgo.It("observes generated scalar and dynamically indexed event subscriptions over WebSocket", func(ctx ginkgo.SpecContext) {
			fixture.assertWebSocketWatcher(ctx)
		})
	},
)
