//go:build e2e

package parity

import (
	"context"
	"math/big"

	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/suites/execution/abi/contracts"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/params"
)

const (
	revertingCallGas = 1_000_000
	withdrawalEpochs = 4
)

// history is what the suite put on chain: the EventEmitter it deployed and
// how many logs its transactions emitted.
type history struct {
	emitter common.Address
	logs    int
}

// sendTransactions sends a transfer, deploys EventEmitter and the library it
// links, calls it to emit several logs, one of them anonymous, and sends a
// call that reverts. It waits until every transaction is in a block.
func sendTransactions(ctx context.Context, node *live.Node) history {
	ginkgo.GinkgoHelper()
	auth := testsuite.MustSucceed(bind.NewKeyedTransactorWithChainID(node.Wallet, node.ChainID))
	auth.Context = ctx

	ginkgo.By("sending a transfer")
	head := testsuite.MustSucceed(node.Execution.HeaderByNumber(ctx, nil))
	tip := testsuite.MustSucceed(node.Execution.SuggestGasTipCap(ctx))
	nonce := testsuite.MustSucceed(node.Execution.PendingNonceAt(ctx, node.Address))
	recipient := common.BytesToAddress([]byte("parity e2e recipient"))
	transfer := testsuite.MustSucceed(types.SignNewTx(node.Wallet, types.NewZondSigner(node.ChainID), &types.DynamicFeeTx{
		ChainID:   node.ChainID,
		Nonce:     nonce,
		GasTipCap: tip,
		// Room for the base fee to double.
		GasFeeCap: new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), tip),
		Gas:       params.TxGas,
		To:        &recipient,
		Value:     big.NewInt(params.Quanta),
	}))
	gomega.Expect(node.Execution.SendTransaction(ctx, transfer)).To(gomega.Succeed())

	ginkgo.By("deploying EventEmitter, whose constructor logs")
	emitter, deployment, binding, err := contracts.DeployEventEmitter(auth, node.Execution,
		big.NewInt(1), "parity", []byte("deployed"),
		contracts.EventEmitterRecord{Amount: big.NewInt(2), Recipient: node.Address, Tag: [64]byte{3}},
		[]uint16{4, 5})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	// The calls' gas is estimated against the deployed contract.
	testsuite.MustSucceed(bind.WaitDeployed(ctx, node.Execution, deployment))

	ginkgo.By("calling EventEmitter to log, and with a call that reverts")
	sent := []*types.Transaction{
		transfer,
		deployment,
		// Two logs each.
		testsuite.MustSucceed(binding.Store(auth, big.NewInt(6), big.NewInt(-7), [64]byte{8}, recipient, []byte("stored"), "first", true)),
		testsuite.MustSucceed(binding.Store(auth, big.NewInt(9), big.NewInt(10), [64]byte{11}, node.Address, nil, "second", false)),
		// An anonymous log, without the event's signature as a topic.
		testsuite.MustSucceed(binding.EmitPinged(auth, 12, big.NewInt(13))),
	}
	// The call's gas is set, since estimating a call that reverts fails.
	reverting := *auth
	reverting.GasLimit = revertingCallGas
	sent = append(sent, testsuite.MustSucceed((&contracts.EventEmitterRaw{Contract: binding}).Transact(&reverting, "failReason")))

	emitted := history{emitter: emitter}
	for index, tx := range sent {
		receipt := testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, tx))
		wantStatus := types.ReceiptStatusSuccessful
		if index == len(sent)-1 {
			wantStatus = types.ReceiptStatusFailed
		}
		gomega.Expect(receipt.Status).To(gomega.Equal(wantStatus), "status of transaction %s", tx.Hash())
		emitted.logs += len(receipt.Logs)
	}
	return emitted
}

// waitForWithdrawals waits until a block has withdrawals. Validators earn
// rewards from the first epoch on, and the chain withdraws what they hold
// above their effective balance.
func waitForWithdrawals(ctx context.Context, node *live.Node, chain chaininfo.Info) {
	ginkgo.GinkgoHelper()
	var next uint64
	gomega.Eventually(func(g gomega.Gomega) bool {
		head, err := node.Execution.BlockNumber(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		for ; next <= head; next++ {
			header, err := node.Execution.HeaderByNumber(ctx, new(big.Int).SetUint64(next))
			g.Expect(err).NotTo(gomega.HaveOccurred())
			if header.WithdrawalsHash != types.EmptyWithdrawalsHash {
				return true
			}
		}
		return false
	}).WithContext(ctx).
		WithTimeout(withdrawalEpochs*chain.EpochDuration).
		WithPolling(chain.SlotDuration).
		Should(gomega.BeTrue(), "no block has withdrawals")
}
