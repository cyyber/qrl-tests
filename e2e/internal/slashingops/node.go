package slashingops

import (
	"context"

	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/validator"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
)

// Node is a devnet participant that runs one of a suite's validator sidecars.
type Node struct {
	*live.Node
	Graffiti string
	client   *validator.Sidecar
}

func (node *Node) StartClient(ctx context.Context, keystores []sidecar.File) {
	ginkgo.GinkgoHelper()
	node.client = testsuite.MustSucceed(validator.Start(ctx, validator.Config{
		Image:              node.ValidatorImage,
		BeaconURL:          node.BeaconURL,
		BeaconGRPC:         node.BeaconGRPC,
		ConsensusServiceID: node.ConsensusServiceID,
		Keystores:          keystores,
		Graffiti:           node.Graffiti,
	}))
}

// StopClient prints the client's last log lines and stops it.
func (node *Node) StopClient() error {
	if node == nil {
		return nil
	}
	client := node.client
	node.client = nil
	if client == nil {
		return nil
	}
	if logs, err := client.Logs(); err != nil {
		ginkgo.GinkgoWriter.Printf("%s validator client logs unavailable: %v\n", node.Graffiti, err)
	} else {
		ginkgo.GinkgoWriter.Printf("%s validator client, last log lines:\n%s\n", node.Graffiti, logs)
	}
	return client.Close()
}

// ReportLogsOnFailure prints the running client's last log lines if the
// current spec failed.
func (node *Node) ReportLogsOnFailure() {
	if node != nil {
		testsuite.ReportLogsOnFailure(node.Graffiti+" validator client", node.client)
	}
}
