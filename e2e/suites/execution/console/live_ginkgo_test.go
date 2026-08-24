//go:build e2e

package console

import (
	"path/filepath"
	"testing"

	endtoendlive "github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Console live E2E suite")
}

var _ = ginkgo.Describe(
	"embedded console against a live qrl-package network",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.ContinueOnFailure,
	ginkgo.Label("e2e", "live", "console", "mutates-chain"),
	func() {
		var (
			jsPath  string
			session *endtoendlive.Node
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			var err error
			runtime := testsuite.LoadRuntime()
			session, err = runtime.PrimaryNode(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Expect(session.ExecutionImage).NotTo(gomega.BeEmpty())

			jsPath = filepath.Join(ginkgo.GinkgoT().TempDir(), "js")
			ginkgo.By("preparing the console scripts")
			gomega.Expect(prepareWorkspace(jsPath)).To(gomega.Succeed())
		})

		for _, scenario := range consoleScenarios {
			ginkgo.It(
				scenario.description,
				func(ctx ginkgo.SpecContext) {
					gomega.Expect(
						runSuite(ctx, session.ExecutionImage, jsPath, session.ExecutionRPCURL, scenario.name),
					).To(gomega.Succeed())
				},
			)
		}
	},
)
