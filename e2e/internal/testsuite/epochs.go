package testsuite

import (
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const maxEpochDuration = 40 * time.Second

// ExpectShortEpochs fails fast on epochs longer than the suites' timeouts
// assume.
func ExpectShortEpochs(chain chaininfo.Info) {
	ginkgo.GinkgoHelper()
	gomega.Expect(chain.EpochDuration).To(gomega.BeNumerically("<=", maxEpochDuration),
		"the suite's timeouts assume epochs of at most %s", maxEpochDuration)
}
