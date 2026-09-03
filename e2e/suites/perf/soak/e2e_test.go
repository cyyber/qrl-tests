//go:build e2e

package soak

import (
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Soak network suite")
}

var suite *liveSuite

var _ = ginkgo.BeforeSuite(func(ctx ginkgo.SpecContext) {
	suite = setupLiveSuite(ctx)
})

var _ = ginkgo.Describe(
	"Soak a live qrl-package network",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "soak"),
	func() {
		ginkgo.It("places one participant on each labelled work node", func(ctx ginkgo.SpecContext) {
			suite.assertPlacement(ctx)
		})

		ginkgo.It("holds chain, peer, RPC, consensus and resource gates for the soak window", func(ctx ginkgo.SpecContext) {
			suite.assertSoak(ctx)
		})
	},
)
