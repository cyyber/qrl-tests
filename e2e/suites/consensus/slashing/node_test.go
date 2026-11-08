//go:build e2e

package slashing

import (
	"context"

	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/validator"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
)

// node is a devnet participant that runs one of the suite's validator sidecars.
type node struct {
	*live.Node
	graffiti string
	client   *validator.Sidecar
}

func (node *node) startClient(ctx context.Context, keystores []sidecar.File) {
	ginkgo.GinkgoHelper()
	node.client = testsuite.MustSucceed(validator.Start(ctx, validator.Config{
		Image:              node.ValidatorImage,
		BeaconURL:          node.BeaconURL,
		BeaconGRPC:         node.BeaconGRPC,
		ConsensusServiceID: node.ConsensusServiceID,
		Keystores:          keystores,
		Graffiti:           node.graffiti,
	}))
}

func (node *node) stopClient() error {
	if node == nil {
		return nil
	}
	client := node.client
	node.client = nil
	if client == nil {
		return nil
	}
	if logs, err := client.Logs(); err != nil {
		ginkgo.GinkgoWriter.Printf("%s validator client logs unavailable: %v\n", node.graffiti, err)
	} else {
		ginkgo.GinkgoWriter.Printf("%s validator client, last log lines:\n%s\n", node.graffiti, logs)
	}
	return client.Close()
}
