//go:build e2e

package stakingoperator

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/keymanager"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/deposit"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/validator"
	"github.com/cyyber/qrl-tests/e2e/internal/staking"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
)

const (
	// The deposit CLI exits 0 even when a send fails, so the deposit event is
	// checked on the execution chain before the long beacon wait.
	depositEventTimeout = time.Minute

	// recipientKeyMarker seeds the withdrawal recipient. It must not collide
	// with the genesis validators or the automated suite's staker marker 0x91.
	recipientKeyMarker = 0x92
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Staking operator E2E suite")
}

var _ = ginkgo.Describe(
	"A new staker using the deposit and validator binaries",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "consensus", "staking-operator", "mutates-chain"),
	func() {
		var (
			node            *live.Node
			chain           chaininfo.Info
			sidecar         *validator.Sidecar
			depositResult   deposit.Result
			publicKey       string
			maximum         uint64
			beaconValidator beacon.Validator
			recipient       common.Address
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			node = testsuite.MustSucceed(testsuite.LoadRuntime().PrimaryNode(ctx))
			gomega.Expect(node.ValidatorImage).NotTo(gomega.BeEmpty(), "validator image is not configured")
			gomega.Expect(node.BeaconGRPC).NotTo(gomega.BeEmpty(), "beacon gRPC is not published")

			chain = testsuite.MustSucceed(chaininfo.Load(ctx, node.Beacon))
			depositor := testsuite.MustSucceed(validatorops.NewDepositor(ctx, node, chain))
			contract := testsuite.MustSucceed(node.Beacon.DepositContract(ctx))
			maximum = testsuite.MustSucceed(node.Beacon.SpecUint(ctx, "MAX_EFFECTIVE_BALANCE"))
			recipientKey := testsuite.MustSucceed(validatorops.DeterministicKey(recipientKeyMarker))
			recipient = recipientKey.Address()
			gomega.Expect(recipient).NotTo(gomega.Equal(node.Address), "withdrawals must use a dedicated recipient")

			ginkgo.By("creating a key and sending its deposit with the deposit CLI")
			fromBlock := testsuite.MustSucceed(node.Execution.BlockNumber(ctx))
			payerSeed := testsuite.MustSucceed(node.Wallet.GetSeed())
			depositResult = testsuite.MustSucceed(deposit.Run(ctx, deposit.Config{
				Image:               deposit.ImageFromEnv(),
				ExecutionRPCURL:     node.ExecutionRPCURL,
				DepositContract:     contract.Address,
				WithdrawalRecipient: recipient.Hex(),
				PayerSeed:           hex.EncodeToString(payerSeed[:]),
				Password:            validatorops.KeystorePassword,
			}))
			publicKey = depositResult.PublicKey
			gomega.Expect(depositResult.Amount).To(gomega.Equal(maximum),
				"deposit CLI amount %d does not match MAX_EFFECTIVE_BALANCE %d", depositResult.Amount, maximum)
			gomega.Eventually(func(g gomega.Gomega) {
				amount, found, err := depositor.DepositedAmount(ctx, common.FromHex(publicKey), fromBlock)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(found).To(gomega.BeTrue(), "the deposit contract has no deposit for %s", publicKey)
				g.Expect(amount).To(gomega.Equal(maximum))
			}).WithContext(ctx).WithTimeout(depositEventTimeout).WithPolling(staking.PollInterval).Should(gomega.Succeed())

			ginkgo.By("starting a validator client with the new keystore")
			sidecar = testsuite.MustSucceed(validator.Start(ctx, validator.Config{
				Image:              node.ValidatorImage,
				BeaconURL:          node.BeaconURL,
				BeaconGRPC:         node.BeaconGRPC,
				ConsensusServiceID: node.ConsensusServiceID,
				Keystores:          depositResult.Keystores,
			}))
			ginkgo.DeferCleanup(sidecar.Close)
			keystores := testsuite.MustSucceed(sidecar.Keymanager.ListKeystores(ctx))
			gomega.Expect(keymanager.ContainsPublicKey(keystores, publicKey)).To(gomega.BeTrue(),
				"imported key is not in the validator client")
		}, ginkgo.NodeTimeout(5*time.Minute))

		// The deposit waits up to chain.DepositWait(), which comes from the
		// live voting period, so this spec has no fixed SpecTimeout; its
		// Eventually timeout bounds it.
		ginkgo.It("deposits the maximum balance through the deposit CLI", func(ctx ginkgo.SpecContext) {
			ginkgo.By("waiting for the deposit to initialize the validator at the maximum balance")
			gomega.Eventually(func(g gomega.Gomega) {
				record, err := node.Beacon.Validator(ctx, publicKey)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(record.Balance).To(gomega.Equal(maximum))
				g.Expect(record.EffectiveBalance).To(gomega.Equal(maximum))
				g.Expect(record.ActivationEpoch).To(gomega.Equal(beacon.FarFutureEpoch))
				beaconValidator = record
			}).WithContext(ctx).WithTimeout(chain.DepositWait()).WithPolling(staking.PollInterval).Should(gomega.Succeed())

			gomega.Expect(strings.EqualFold(beaconValidator.PublicKey, publicKey)).To(gomega.BeTrue())
			gomega.Expect(strings.EqualFold(beaconValidator.WithdrawalRecipient, hexutil.Encode(recipient[:]))).To(gomega.BeTrue(),
				"withdrawal recipient %s is not the staker wallet", beaconValidator.WithdrawalRecipient)
			gomega.Expect(strings.EqualFold(beaconValidator.RandaoCommitment, depositResult.RandaoCommitment)).To(gomega.BeTrue(),
				"validator record carries a different RANDAO commitment than the deposit CLI")
		})

		ginkgo.It("activates the validator and schedules it for attestation duties", func(ctx ginkgo.SpecContext) {
			beaconValidator = staking.ExpectActiveAndAttesting(ctx, node, chain, publicKey)
		}, ginkgo.SpecTimeout(staking.ActivationTimeout))

		ginkgo.It("exits the validator through the accounts CLI and withdraws its stake", func(ctx ginkgo.SpecContext) {
			beaconValidator = staking.ExpectExitAndWithdrawal(ctx, node, chain, beaconValidator, recipient,
				func(ctx context.Context, _ uint64) error { return sidecar.VoluntaryExit(ctx, publicKey) })
		}, ginkgo.SpecTimeout(staking.ExitTimeout))
	},
)
