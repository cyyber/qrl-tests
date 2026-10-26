// Package testsuite provides the common Ginkgo entrypoint used by E2E packages.
package testsuite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/manifest"
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

// ReportLogsOnFailure writes the end of a sidecar's output to the report of a
// failed spec. Call it from an AfterEach: sidecars are not Kurtosis services,
// so the lane diagnostics do not collect their logs.
func ReportLogsOnFailure(name string, sidecar interface{ Logs() (string, error) }) {
	if !ginkgo.CurrentSpecReport().Failed() {
		return
	}
	logs, err := sidecar.Logs()
	switch {
	case err != nil:
		ginkgo.GinkgoWriter.Printf("%s logs unavailable: %v\n", name, err)
	case logs != "":
		ginkgo.GinkgoWriter.Printf("%s, last log lines:\n%s\n", name, logs)
	}
}

func LoadRuntime() *live.Runtime {
	ginkgo.GinkgoHelper()

	runtime := MustSucceed(live.Load())
	ginkgo.DeferCleanup(runtime.Close)
	return runtime
}

// LaneReportDir returns the lane's report directory, where the runner writes
// the lane manifest and a suite may save files of its own.
func LaneReportDir() string {
	ginkgo.GinkgoHelper()
	path := os.Getenv(manifest.PathEnv)
	gomega.Expect(path).NotTo(gomega.BeEmpty(), "%s is not configured", manifest.PathEnv)
	return filepath.Dir(path)
}
