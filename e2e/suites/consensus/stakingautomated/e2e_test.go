//go:build e2e

package stakingautomated

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/keymanager"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/validator"
	"github.com/cyyber/qrl-tests/e2e/internal/staking"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
)

// stakerKeyMarker seeds the staker's validator key; it must not collide with
// the genesis validators, which derive from the package mnemonic.
const stakerKeyMarker = 0x91

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Staking automated E2E suite")
}

var _ = ginkgo.Describe(
	"A new staker through the protocol APIs",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "consensus", "staking-automated", "mutates-chain"),
	func() {
		var (
			node            *live.Node
			chain           chaininfo.Info
			sidecar         *validator.Sidecar
			depositor       *validatorops.Depositor
			key             *validatorops.Key
			publicKey       string
			maximum         uint64
			beaconValidator beacon.Validator
			recipient       common.Address
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			node = testsuite.MustSucceed(testsuite.LoadRuntime().PrimaryNode(ctx))
			gomega.Expect(node.ValidatorImage).NotTo(gomega.BeEmpty(), "validator image is not configured")
			gomega.Expect(node.BeaconGRPC).NotTo(gomega.BeEmpty(), "beacon gRPC is not published")
			sidecar = testsuite.MustSucceed(validator.Start(ctx, validator.Config{
				Image:              node.ValidatorImage,
				BeaconURL:          node.BeaconURL,
				BeaconGRPC:         node.BeaconGRPC,
				ConsensusServiceID: node.ConsensusServiceID,
			}))
			ginkgo.DeferCleanup(sidecar.Close)
			chain = testsuite.MustSucceed(chaininfo.Load(ctx, node.Beacon))
			depositor = testsuite.MustSucceed(validatorops.NewDepositor(ctx, node, chain))
			key = testsuite.MustSucceed(validatorops.DeterministicKey(stakerKeyMarker))
			recipient = key.Address()
			gomega.Expect(recipient).NotTo(gomega.Equal(node.Address), "withdrawals must use a dedicated recipient")
			publicKey = hexutil.Encode(key.PublicKey())
			maximum = testsuite.MustSucceed(node.Beacon.SpecUint(ctx, "MAX_EFFECTIVE_BALANCE"))

			_, err := node.Beacon.Validator(ctx, publicKey)
			gomega.Expect(beacon.IsNotFound(err)).To(gomega.BeTrue(), "staker key is already a validator: %v", err)
		}, ginkgo.NodeTimeout(2*time.Minute))

		// The wait is sized from the live voting period, so this spec has no
		// fixed SpecTimeout; its Eventually timeout bounds it.
		//
		// Both deposits are submitted before waiting, so the chain usually
		// picks them up in one voting period. That skips checking that the
		// half-funded validator stays pending_initialized without activating.
		// TODO: cover that state, which needs a second voting-period wait.
		ginkgo.It("deposits half the maximum balance and tops it up to the maximum", func(ctx ginkgo.SpecContext) {
			first := maximum / 2
			second := maximum - first

			ginkgo.By("submitting the initial deposit")
			_, err := depositor.Deposit(ctx, key, recipient, first)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("submitting the top-up deposit")
			_, err = depositor.Deposit(ctx, key, recipient, second)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// The validator client accepts a key before its validator exists, so
			// a keymanager failure shows up before the long wait.
			ginkgo.By("importing the staker key into the operator validator client")
			keystore := testsuite.MustSucceed(key.KeystoreJSON(validatorops.KeystorePassword))
			gomega.Expect(sidecar.Keymanager.ImportKeystore(ctx, keystore, validatorops.KeystorePassword)).To(gomega.Succeed())
			keystores := testsuite.MustSucceed(sidecar.Keymanager.ListKeystores(ctx))
			gomega.Expect(keymanager.ContainsPublicKey(keystores, publicKey)).To(gomega.BeTrue(), "imported key is not in the validator client")

			// On a fresh devnet both deposits land in its first voting period,
			// so one wait covers them.
			ginkgo.By("waiting for one validator at the maximum balance")
			gomega.Eventually(func(g gomega.Gomega) {
				record, err := node.Beacon.Validator(ctx, publicKey)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(record.Balance).To(gomega.Equal(maximum))
				g.Expect(record.EffectiveBalance).To(gomega.Equal(maximum))
				beaconValidator = record
			}).WithContext(ctx).WithTimeout(chain.DepositWait()).WithPolling(staking.PollInterval).Should(gomega.Succeed())

			gomega.Expect(beaconValidator.PublicKey).To(gomega.Equal(publicKey))
			gomega.Expect(strings.EqualFold(beaconValidator.WithdrawalRecipient, hexutil.Encode(recipient[:]))).To(gomega.BeTrue(),
				"withdrawal recipient %s is not the staker wallet", beaconValidator.WithdrawalRecipient)
			gomega.Expect(strings.EqualFold(beaconValidator.RandaoCommitment, hexutil.Encode(key.RandaoCommitment()))).To(gomega.BeTrue(),
				"validator record carries a different RANDAO commitment than the deposit")
		})

		ginkgo.It("activates the validator and schedules it for attestation duties", func(ctx ginkgo.SpecContext) {
			beaconValidator = staking.ExpectActiveAndAttesting(ctx, node, chain, publicKey)
		}, ginkgo.SpecTimeout(staking.ActivationTimeout))

		ginkgo.It("exits the validator and withdraws its stake to the staker wallet", func(ctx ginkgo.SpecContext) {
			beaconValidator = staking.ExpectExitAndWithdrawal(ctx, node, chain, beaconValidator, recipient,
				func(ctx context.Context, epoch uint64) error {
					exit, err := sidecar.Keymanager.SignVoluntaryExit(ctx, publicKey, epoch)
					if err != nil {
						return err
					}
					return node.Beacon.SubmitVoluntaryExit(ctx, exit)
				})
		}, ginkgo.SpecTimeout(staking.ExitTimeout))
	},
)
