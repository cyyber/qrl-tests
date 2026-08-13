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
			gqrlPath string
			jsPath   string
			rpcURL   string
			session  *endtoendlive.Node
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			var err error
			runtime := testsuite.LoadRuntime()
			session, err = runtime.PrimaryWithWebSocket(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			rpcURL = session.Participant.Execution.RPCURL

			workDir := ginkgo.GinkgoT().TempDir()

			gqrlPath, err = runtime.GQRL()
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			jsPath = filepath.Join(workDir, "js")
			ginkgo.By("preparing the console scripts and deployment transaction")
			gomega.Expect(prepareWorkspace(ctx, jsPath, session)).To(gomega.Succeed())
		})

		for _, scenario := range consoleScenarios {
			ginkgo.It(
				scenario.description,
				func(ctx ginkgo.SpecContext) {
					if scenario.webSocket {
						gomega.Expect(runWatchedSuite(
							ctx,
							session.ExecutionWebSocket.Client(),
							jsPath,
							scenario.name,
						)).To(gomega.Succeed())
						return
					}
					gomega.Expect(
						runSuite(ctx, gqrlPath, jsPath, rpcURL, scenario.name),
					).To(gomega.Succeed())
				},
			)
		}
	},
)
