// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

// Package testsuite provides the common Ginkgo entrypoint used by E2E packages.
package testsuite

import (
	"testing"

	endtoendlive "github.com/cyyber/qrl-tests/endtoend/internal/live"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

func Run(t *testing.T, name string) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, name)
}

func LoadRuntime() *endtoendlive.Runtime {
	ginkgo.GinkgoHelper()

	runtime, err := endtoendlive.Load()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	ginkgo.DeferCleanup(runtime.Close)
	return runtime
}
