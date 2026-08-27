//go:build e2e

package console

import (
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Console E2E suite")
}

var _ = ginkgo.Describe(
	"gqrl console against a live qrl-package network",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.ContinueOnFailure,
	ginkgo.Label("e2e", "console"),
	func() {
		var (
			jsPath string
			node   *live.Node
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			var err error
			runtime := testsuite.LoadRuntime()
			node, err = runtime.PrimaryNode(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Expect(node.ExecutionImage).NotTo(gomega.BeEmpty())

			jsPath = ginkgo.GinkgoT().TempDir()
			ginkgo.By("preparing the console scripts")
			gomega.Expect(prepareWorkspace(jsPath, node.NetworkExpectations)).To(gomega.Succeed())
		})

		ginkgo.It("validates console and RPC APIs against the live network", func(ctx ginkgo.SpecContext) {
			gomega.Expect(
				runSuite(ctx, node.ExecutionImage, jsPath, node.ExecutionRPCURL, "api"),
			).To(gomega.Succeed())
		})
	},
)
