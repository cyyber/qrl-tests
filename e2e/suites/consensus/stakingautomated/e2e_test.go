//go:build e2e

package stakingautomated

import (
	"context"
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
	"A new staker using the deposit contract and keymanager API",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "consensus", "staking-automated"),
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
			maximum = testsuite.MustSucceed(node.Beacon.SpecUint(ctx, "MAX_EFFECTIVE_BALANCE"))

			key = testsuite.MustSucceed(validatorops.DeterministicKey(stakerKeyMarker))
			publicKey = hexutil.Encode(key.PublicKey())
			recipient = key.Address()
			gomega.Expect(recipient).NotTo(gomega.Equal(node.Address), "withdrawals must use a dedicated recipient")

			_, err := node.Beacon.Validator(ctx, publicKey)
			gomega.Expect(err).To(gomega.Satisfy(beacon.IsNotFound), "the staker key must not be a validator yet")
		}, ginkgo.NodeTimeout(2*time.Minute))

		ginkgo.AfterEach(func() {
			testsuite.ReportLogsOnFailure("validator client", sidecar)
		})

		// No SpecTimeout: the wait is sized from the live voting period.
		ginkgo.It("deposits the maximum balance through the deposit contract", func(ctx ginkgo.SpecContext) {
			ginkgo.By("submitting the deposit")
			testsuite.MustSucceed(depositor.Deposit(ctx, key, recipient, maximum))

			// The validator client accepts a key before its validator exists, so
			// a keymanager failure shows up before the long wait.
			ginkgo.By("importing the staker key through the keymanager API")
			keystore := testsuite.MustSucceed(key.KeystoreJSON(validatorops.KeystorePassword))
			err := sidecar.Keymanager.ImportKeystore(ctx, keystore, validatorops.KeystorePassword)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			keystores := testsuite.MustSucceed(sidecar.Keymanager.ListKeystores(ctx))
			gomega.Expect(keymanager.ContainsPublicKey(keystores, publicKey)).To(gomega.BeTrue(),
				"imported key is not in the validator client")

			beaconValidator = staking.ExpectDeposited(ctx, node, chain, publicKey, maximum, recipient,
				hexutil.Encode(key.RandaoCommitment()))
		})

		ginkgo.It("activates the validator and attests", func(ctx ginkgo.SpecContext) {
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
