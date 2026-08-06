// Package testsuite provides the common Ginkgo entrypoint used by E2E packages.
package testsuite

import (
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

func Run(t *testing.T, name string) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, name)
}

func LoadRuntime() *live.Runtime {
	ginkgo.GinkgoHelper()

	runtime, err := live.Load()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	ginkgo.DeferCleanup(runtime.Close)
	return runtime
}
