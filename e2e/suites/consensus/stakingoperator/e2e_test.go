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
	// cliTimeout bounds the deposit CLI run and the validator client start.
	// The spec they run in has no timeout of its own.
	cliTimeout = 5 * time.Minute

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
	ginkgo.Label("e2e", "consensus", "staking-operator"),
	func() {
		var (
			node            *live.Node
			chain           chaininfo.Info
			depositor       *validatorops.Depositor
			sidecar         *validator.Sidecar
			publicKey       string
			maximum         uint64
			beaconValidator beacon.Validator
			recipient       common.Address
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			node = testsuite.MustSucceed(testsuite.LoadRuntime().PrimaryNode(ctx))
			gomega.Expect(node.ValidatorImage).NotTo(gomega.BeEmpty(), "validator image is not configured")
			gomega.Expect(node.QrysmAlltoolsImage).NotTo(gomega.BeEmpty(), "Qrysm all-tools image is not configured")
			gomega.Expect(node.BeaconGRPC).NotTo(gomega.BeEmpty(), "beacon gRPC is not published")

			chain = testsuite.MustSucceed(chaininfo.Load(ctx, node.Beacon))
			depositor = testsuite.MustSucceed(validatorops.NewDepositor(ctx, node, chain))
			maximum = testsuite.MustSucceed(node.Beacon.SpecUint(ctx, "MAX_EFFECTIVE_BALANCE"))

			recipientKey := testsuite.MustSucceed(validatorops.DeterministicKey(recipientKeyMarker))
			recipient = recipientKey.Address()
			gomega.Expect(recipient).NotTo(gomega.Equal(node.Address), "withdrawals must use a dedicated recipient")
		}, ginkgo.NodeTimeout(2*time.Minute))

		ginkgo.AfterEach(func() {
			testsuite.ReportLogsOnFailure("validator client", sidecar)
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(sidecar.Close()).To(gomega.Succeed())
		})

		// No SpecTimeout: the wait is sized from the live voting period.
		ginkgo.It("deposits the maximum balance through the deposit CLI", func(ctx ginkgo.SpecContext) {
			cliCtx, cancel := context.WithTimeout(ctx, cliTimeout)
			defer cancel()

			ginkgo.By("creating a key and sending its deposit with the deposit CLI")
			contract := testsuite.MustSucceed(node.Beacon.DepositContract(cliCtx))
			fromBlock := testsuite.MustSucceed(node.Execution.BlockNumber(cliCtx))
			payerSeed := testsuite.MustSucceed(node.Wallet.GetSeed())

			deposited := testsuite.MustSucceed(deposit.Run(cliCtx, deposit.Config{
				Image:               node.QrysmAlltoolsImage,
				ExecutionRPCURL:     node.ExecutionRPCURL,
				DepositContract:     contract.Address,
				WithdrawalRecipient: recipient.Hex(),
				PayerSeed:           hex.EncodeToString(payerSeed[:]),
				Password:            validatorops.KeystorePassword,
			}))
			publicKey = deposited.PublicKey

			// Printed only if the spec fails. The CLI exits 0 even when a send
			// fails, and only its output says why.
			ginkgo.GinkgoWriter.Printf("deposit CLI output:\n%s\n", deposited.Output)

			ginkgo.By("checking the deposit data the CLI wrote")
			gomega.Expect(deposited.Amount).To(gomega.Equal(maximum),
				"deposit CLI amount %d does not match MAX_EFFECTIVE_BALANCE %d", deposited.Amount, maximum)

			recipientHex := hexutil.Encode(recipient[:])
			gomega.Expect(strings.EqualFold(deposited.WithdrawalRecipient, recipientHex)).To(gomega.BeTrue(),
				"deposit CLI wrote withdrawal recipient %s, want %s", deposited.WithdrawalRecipient, recipientHex)

			// The CLI signs for its compiled-in dev chain, which must match the
			// network's. On a mismatch the contract still logs the deposit and the
			// beacon chain ignores it, so the spec would only time out after the
			// long wait.
			gomega.Expect(strings.EqualFold(deposited.ForkVersion, chain.GenesisForkVersion())).To(gomega.BeTrue(),
				"deposit CLI signed for fork version %s, the network's is %s",
				deposited.ForkVersion, chain.GenesisForkVersion())

			ginkgo.By("waiting for the deposit contract to log the deposit")
			gomega.Eventually(func(g gomega.Gomega) {
				amount, found, err := depositor.DepositedAmount(cliCtx, common.FromHex(publicKey), fromBlock)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(found).To(gomega.BeTrue(), "the deposit contract has not logged the deposit")
				g.Expect(amount).To(gomega.Equal(maximum))
			}).WithContext(cliCtx).
				WithTimeout(depositEventTimeout).
				WithPolling(staking.PollInterval).
				Should(gomega.Succeed())

			ginkgo.By("starting a validator client with the new keystore")
			sidecar = testsuite.MustSucceed(validator.Start(cliCtx, validator.Config{
				Image:              node.ValidatorImage,
				BeaconURL:          node.BeaconURL,
				BeaconGRPC:         node.BeaconGRPC,
				ConsensusServiceID: node.ConsensusServiceID,
				Keystores:          deposited.Keystores,
			}))

			keystores := testsuite.MustSucceed(sidecar.Keymanager.ListKeystores(cliCtx))
			gomega.Expect(keymanager.ContainsPublicKey(keystores, publicKey)).To(gomega.BeTrue(),
				"imported key is not in the validator client")

			beaconValidator = staking.ExpectDeposited(ctx, node, chain, publicKey, maximum, recipient,
				deposited.RandaoCommitment)
		})

		ginkgo.It("activates the validator and attests", func(ctx ginkgo.SpecContext) {
			beaconValidator = staking.ExpectActiveAndAttesting(ctx, node, chain, publicKey)
		}, ginkgo.SpecTimeout(staking.ActivationTimeout))

		ginkgo.It("exits the validator with the voluntary-exit command and withdraws its stake", func(ctx ginkgo.SpecContext) {
			beaconValidator = staking.ExpectExitAndWithdrawal(ctx, node, chain, beaconValidator, recipient,
				func(ctx context.Context, _ uint64) error { return sidecar.VoluntaryExit(ctx, publicKey) })
		}, ginkgo.SpecTimeout(staking.ExitTimeout))
	},
)
