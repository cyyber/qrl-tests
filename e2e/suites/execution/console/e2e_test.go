//go:build e2e

package console

import (
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/suites/execution/console/contracts"
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
		var node *live.Node

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			var err error
			node, err = testsuite.LoadRuntime().PrimaryNode(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Expect(node.ExecutionImage).NotTo(gomega.BeEmpty())
		})

		ginkgo.It("validates console and RPC APIs against the live network", func(ctx ginkgo.SpecContext) {
			fixtureArchive, err := consoleFixtureArchive(nil)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(
				runScenario(ctx, consoleContainerConfig{
					image:       node.ExecutionImage,
					endpointURL: node.ExecutionRPCURL,
					scenario:    "api",
				}, fixtureArchive),
			).To(gomega.Succeed())
		})

		ginkgo.It("deploys a contract and validates VM64 ABI, receipts, events, and filters", func(ctx ginkgo.SpecContext) {
			ginkgo.By("preparing the contract fixture")
			bytecode, err := contracts.ConsoleProbeBytecode()
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			parameters, err := prepareContractParameters(ctx, node, contracts.ConsoleProbeABI, bytecode)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			fixtureArchive, err := consoleFixtureArchive(parameters)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(
				runScenario(ctx, consoleContainerConfig{
					image:       node.ExecutionImage,
					endpointURL: node.ExecutionRPCURL,
					scenario:    "contract",
				}, fixtureArchive),
			).To(gomega.Succeed())
		})

		ginkgo.It("encodes and decodes indexed VM64 topics", func(ctx ginkgo.SpecContext) {
			ginkgo.By("preparing the indexed topic fixture")
			parameters, err := prepareTopicParameters(ctx, node)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			fixtureArchive, err := consoleFixtureArchive(parameters)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(
				runScenario(ctx, consoleContainerConfig{
					image:       node.ExecutionImage,
					endpointURL: node.ExecutionRPCURL,
					scenario:    "topics",
				}, fixtureArchive),
			).To(gomega.Succeed())
		})
	},
)
