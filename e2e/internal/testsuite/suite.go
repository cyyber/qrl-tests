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

// MustSucceed asserts that err is nil and returns value.
func MustSucceed[T any](value T, err error) T {
	ginkgo.GinkgoHelper()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return value
}

func LoadRuntime() *live.Runtime {
	ginkgo.GinkgoHelper()

	runtime := MustSucceed(live.Load())
	ginkgo.DeferCleanup(runtime.Close)
	return runtime
}
