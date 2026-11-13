//go:build e2e

package rpc

import (
	"context"
	"fmt"

	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/params"
)

// expectPoolContent reads the pool, which has the transaction that stays in
// it: under its sender and nonce in the content, alone in its sender's
// content, summarized in the inspection and counted in the status.
func expectPoolContent(ctx context.Context, node *live.Node, sender, to common.Address, pending *types.Transaction) {
	ginkgo.GinkgoHelper()
	const nonce = "0"

	content := callObject(ctx, node, "txpool_content")
	gomega.Expect(content).To(gomega.HaveKey("queued"))
	gomega.Expect(content["pending"]).To(gomega.HaveKey(sender.Hex()))
	bySender := content["pending"].(map[string]any)[sender.Hex()].(map[string]any)
	gomega.Expect(bySender).To(gomega.HaveLen(1))
	gomega.Expect(bySender).To(gomega.HaveKey(nonce))
	tx := bySender[nonce].(map[string]any)
	gomega.Expect(tx).To(gomega.HaveKeyWithValue("hash", pending.Hash().Hex()))
	gomega.Expect(tx).To(gomega.HaveKeyWithValue("from", sender.Hex()))
	expectFields(tx, transactionFields...)
	for _, field := range []string{"blockHash", "blockNumber", "transactionIndex"} {
		gomega.Expect(tx).To(gomega.HaveKeyWithValue(field, gomega.BeNil()), "field %q", field)
	}

	gomega.Expect(callObject(ctx, node, "txpool_contentFrom", sender)).To(gomega.Equal(map[string]any{
		"pending": map[string]any{nonce: tx},
		"queued":  map[string]any{},
	}))

	inspection := callObject(ctx, node, "txpool_inspect")
	gomega.Expect(inspection).To(gomega.HaveKey("queued"))
	gomega.Expect(inspection["pending"]).To(gomega.HaveKeyWithValue(sender.Hex(), map[string]any{
		nonce: fmt.Sprintf("%s: %s planck + %d gas × %s planck", to.Hex(), pending.Value(), params.TxGas, pending.GasFeeCap()),
	}))

	status := callObject(ctx, node, "txpool_status")
	gomega.Expect(status).To(gomega.HaveKey("queued"))
	gomega.Expect(testsuite.MustSucceed(hexutil.DecodeUint64(status["pending"].(string)))).To(gomega.BeNumerically(">=", 1))
}
