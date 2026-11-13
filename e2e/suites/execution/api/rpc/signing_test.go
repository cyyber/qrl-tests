//go:build e2e

package rpc

import (
	"context"
	"math/big"

	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/suites/execution/abi/contracts"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	qrl "github.com/theQRL/go-qrl"
	"github.com/theQRL/go-qrl/accounts"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/crypto/pqcrypto"
	"github.com/theQRL/go-qrl/params"
	"github.com/theQRL/go-qrl/qrlclient/gqrlclient"
)

// transactionResult is what qrl_fillTransaction and qrl_signTransaction
// return: a transaction and its encoding.
type transactionResult struct {
	Raw hexutil.Bytes      `json:"raw"`
	Tx  *types.Transaction `json:"tx"`
}

// expectAccessLists creates the access list of a transfer, which is empty,
// and of a call that writes EventEmitter's storage, which names the contract
// and the keys it writes.
func expectAccessLists(ctx context.Context, node *live.Node, to, emitter common.Address) {
	ginkgo.GinkgoHelper()
	client := gqrlclient.New(node.Execution.Client())

	accessList, gasUsed, vmError, err := client.CreateAccessList(ctx, qrl.CallMsg{From: node.Address, To: &to, Value: big.NewInt(1)})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(vmError).To(gomega.BeEmpty())
	gomega.Expect(*accessList).To(gomega.BeEmpty())
	gomega.Expect(gasUsed).To(gomega.Equal(params.TxGas))

	parsed := testsuite.MustSucceed(contracts.EventEmitterMetaData.GetAbi())
	store := testsuite.MustSucceed(parsed.Pack("store", big.NewInt(6), big.NewInt(-7), [64]byte{8}, to, []byte("stored"), "listed", true))
	accessList, gasUsed, vmError, err = client.CreateAccessList(ctx, qrl.CallMsg{From: node.Address, To: &emitter, Data: store})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(vmError).To(gomega.BeEmpty())
	gomega.Expect(*accessList).To(gomega.HaveLen(1))
	gomega.Expect((*accessList)[0].Address).To(gomega.Equal(emitter))
	gomega.Expect((*accessList)[0].StorageKeys).NotTo(gomega.BeEmpty())
	gomega.Expect(gasUsed).To(gomega.BeNumerically(">", params.TxGas))
}

// expectFilledAndSigned has the node fill a transfer from its account with
// the nonce, gas and fees it lacks, then sign it. A block includes the signed
// transfer.
func expectFilledAndSigned(ctx context.Context, node *live.Node, to common.Address) {
	ginkgo.GinkgoHelper()
	nonce := testsuite.MustSucceed(node.Execution.PendingNonceAt(ctx, node.Address))
	transfer := map[string]any{"from": node.Address, "to": to, "value": "0x1"}

	ginkgo.By("filling the transfer")
	filled := call[transactionResult](ctx, node, "qrl_fillTransaction", transfer)
	gomega.Expect(filled.Tx.ChainId()).To(gomega.Equal(node.ChainID))
	gomega.Expect(filled.Tx.Nonce()).To(gomega.Equal(nonce))
	gomega.Expect(filled.Tx.Gas()).To(gomega.Equal(params.TxGas))
	gomega.Expect(filled.Tx.To()).To(gomega.Equal(&to))
	gomega.Expect(filled.Tx.Value()).To(gomega.Equal(big.NewInt(1)))
	gomega.Expect(filled.Tx.GasTipCap().Sign()).To(gomega.Equal(1))
	gomega.Expect(filled.Tx.GasFeeCap().Cmp(filled.Tx.GasTipCap())).To(gomega.BeNumerically(">=", 0))
	expectEncoding(filled)

	ginkgo.By("signing the filled transfer")
	transfer["nonce"] = hexutil.Uint64(filled.Tx.Nonce())
	transfer["gas"] = hexutil.Uint64(filled.Tx.Gas())
	transfer["maxFeePerGas"] = (*hexutil.Big)(filled.Tx.GasFeeCap())
	transfer["maxPriorityFeePerGas"] = (*hexutil.Big)(filled.Tx.GasTipCap())
	signed := call[transactionResult](ctx, node, "qrl_signTransaction", transfer)
	gomega.Expect(types.Sender(types.NewZondSigner(node.ChainID), signed.Tx)).To(gomega.Equal(node.Address))
	expectEncoding(signed)

	ginkgo.By("sending the signed transfer")
	gomega.Expect(node.Execution.SendTransaction(ctx, signed.Tx)).To(gomega.Succeed())
	receipt := testsuite.MustSucceed(bind.WaitMined(ctx, node.Execution, signed.Tx))
	gomega.Expect(receipt.Status).To(gomega.Equal(types.ReceiptStatusSuccessful))
}

// expectEncoding fails unless the result's encoding is its transaction's.
func expectEncoding(result transactionResult) {
	ginkgo.GinkgoHelper()
	var decoded types.Transaction
	gomega.Expect(decoded.UnmarshalBinary(result.Raw)).To(gomega.Succeed())
	gomega.Expect(decoded.Hash()).To(gomega.Equal(result.Tx.Hash()))
}

// expectSignedMessage has the node sign a message with its account, which is
// the shared wallet: the signature is the wallet's over the message's text
// hash.
func expectSignedMessage(ctx context.Context, node *live.Node) {
	ginkgo.GinkgoHelper()
	message := []byte("rpc e2e message")
	signature := call[hexutil.Bytes](ctx, node, "qrl_sign", node.Address, hexutil.Bytes(message))
	gomega.Expect(pqcrypto.MLDSA87VerifySignature(signature, accounts.TextHash(message), node.Wallet.GetPK(), node.Wallet.GetDescriptor())).To(gomega.BeTrue())
}
