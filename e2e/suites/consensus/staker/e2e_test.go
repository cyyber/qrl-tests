//go:build e2e

package staker

import (
	"math/big"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/consensuscontext"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/params"
)

const (
	pollInterval = 2 * time.Second

	// The single profile has 40-second epochs and 160-second voting periods.
	// Qrysm's 60-second execution block time and follow distance of 8 impose
	// a 16-minute genesis voting floor. Later deposits need 8 minutes of
	// follow distance plus voting-period alignment and a majority of votes.
	initialDepositTimeout = 20 * time.Minute
	topUpTimeout          = 15 * time.Minute

	// Activation waits for eligibility finalization and the seed lookahead.
	// Exit waits two committee epochs, the five-epoch exit lookahead, and
	// two withdrawability epochs: about six minutes on a healthy network.
	activationTimeout = 8 * time.Minute
	exitTimeout       = 8 * time.Minute

	// stakerKeyMarker seeds the staker's validator key; it must not collide
	// with the genesis validators, which derive from the package mnemonic.
	stakerKeyMarker = 0x91
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Staker lifecycle E2E suite")
}

var _ = ginkgo.Describe(
	"A new staker against a live qrl-package network",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "consensus", "staker", "mutates-chain"),
	func() {
		var (
			node         *live.Node
			chain        consensuscontext.Context
			depositor    *validatorops.Depositor
			key          *validatorops.Key
			publicKey    string
			maximum      uint64
			validator    beacon.Validator
			recipient    common.Address
			recipientHex string
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			node = testsuite.MustSucceed(testsuite.LoadRuntime().PrimaryNode(ctx))
			chain = testsuite.MustSucceed(consensuscontext.Load(ctx, node.Beacon))
			depositor = testsuite.MustSucceed(validatorops.NewDepositor(ctx, node, chain))
			key = testsuite.MustSucceed(validatorops.DeterministicKey(stakerKeyMarker))
			recipient = key.Address()
			recipientHex = hexutil.Encode(recipient[:])
			gomega.Expect(recipient).NotTo(gomega.Equal(node.Address), "withdrawals must use a dedicated recipient")
			publicKey = hexutil.Encode(key.PublicKey())
			maximum = testsuite.MustSucceed(node.Beacon.SpecUint(ctx, "MAX_EFFECTIVE_BALANCE"))

			_, err := node.Beacon.Validator(ctx, publicKey)
			gomega.Expect(beacon.IsNotFound(err)).To(gomega.BeTrue(), "staker key is already a validator: %v", err)
		})

		ginkgo.It("deposits half the maximum balance and tops it up to the maximum", func(ctx ginkgo.SpecContext) {
			first := maximum / 2
			second := maximum - first

			ginkgo.By("submitting the initial deposit")
			_, err := depositor.Deposit(ctx, key, recipient, first)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("waiting for the half-funded validator to be initialized without activation")
			gomega.Eventually(func(g gomega.Gomega) {
				record, err := node.Beacon.Validator(ctx, publicKey)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(record.Balance).To(gomega.Equal(first))
				g.Expect(record.EffectiveBalance).To(gomega.Equal(first))
				g.Expect(record.Status).To(gomega.Equal("pending_initialized"))
				g.Expect(record.ActivationEpoch).To(gomega.Equal(beacon.FarFutureEpoch))
				validator = record
			}).WithContext(ctx).WithTimeout(initialDepositTimeout).WithPolling(pollInterval).Should(gomega.Succeed())
			initialIndex := validator.Index

			ginkgo.By("submitting the top-up deposit")
			_, err = depositor.Deposit(ctx, key, recipient, second)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("waiting for the top-up to bring the same validator to the maximum balance")
			gomega.Eventually(func(g gomega.Gomega) {
				record, err := node.Beacon.Validator(ctx, publicKey)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(record.Index).To(gomega.Equal(initialIndex))
				g.Expect(record.Balance).To(gomega.Equal(maximum))
				g.Expect(record.EffectiveBalance).To(gomega.Equal(maximum))
				validator = record
			}).WithContext(ctx).WithTimeout(topUpTimeout).WithPolling(pollInterval).Should(gomega.Succeed())

			gomega.Expect(validator.PublicKey).To(gomega.Equal(publicKey))
			gomega.Expect(strings.EqualFold(validator.WithdrawalRecipient, recipientHex)).To(gomega.BeTrue(),
				"withdrawal recipient %s is not the staker wallet", validator.WithdrawalRecipient)
			gomega.Expect(strings.EqualFold(validator.RandaoCommitment, hexutil.Encode(key.RandaoCommitment()))).To(gomega.BeTrue(),
				"validator record carries a different RANDAO commitment than the deposit")
		}, ginkgo.SpecTimeout(initialDepositTimeout+topUpTimeout))

		ginkgo.It("activates the validator and schedules it for attestation duties", func(ctx ginkgo.SpecContext) {
			ginkgo.By("waiting for the activation queue")
			gomega.Eventually(func(g gomega.Gomega) {
				record, err := node.Beacon.Validator(ctx, publicKey)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(record.Status).To(gomega.Equal("active_ongoing"))
				validator = record
			}).WithContext(ctx).WithTimeout(activationTimeout).WithPolling(pollInterval).Should(gomega.Succeed())

			gomega.Expect(validator.ActivationEpoch).NotTo(gomega.Equal(beacon.FarFutureEpoch))
			gomega.Expect(validator.ExitEpoch).To(gomega.Equal(beacon.FarFutureEpoch))
			gomega.Expect(validator.Slashed).To(gomega.BeFalse())

			ginkgo.By("checking the validator is assigned attestation duties")
			head := testsuite.MustSucceed(node.Beacon.Head(ctx))
			duties := testsuite.MustSucceed(node.Beacon.AttesterDuties(ctx, chain.Epoch(head.Slot), []uint64{validator.Index}))
			gomega.Expect(duties).To(gomega.HaveLen(1))
			gomega.Expect(duties[0].ValidatorIndex).To(gomega.Equal(validator.Index))
			gomega.Expect(strings.EqualFold(duties[0].PublicKey, publicKey)).To(gomega.BeTrue())
		}, ginkgo.SpecTimeout(activationTimeout))

		ginkgo.It("exits the validator and withdraws its stake to the staker wallet", func(ctx ginkgo.SpecContext) {
			gomega.Expect(validator.Status).To(gomega.Equal("active_ongoing"), "the activation spec must pass first")
			committeePeriod := testsuite.MustSucceed(node.Beacon.SpecUint(ctx, "SHARD_COMMITTEE_PERIOD"))

			ginkgo.By("waiting until the validator has been active for the committee period")
			gomega.Eventually(func(g gomega.Gomega) {
				head, err := node.Beacon.HeadSlot(ctx)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(chain.Epoch(head)).To(gomega.BeNumerically(">=", validator.ActivationEpoch+committeePeriod))
			}).WithContext(ctx).WithTimeout(exitTimeout).WithPolling(pollInterval).Should(gomega.Succeed())

			ginkgo.By("submitting the voluntary exit")
			balanceBefore := testsuite.MustSucceed(node.Execution.BalanceAt(ctx, recipient, nil))
			gomega.Expect(balanceBefore.Sign()).To(gomega.BeZero(), "the dedicated recipient must be unfunded")
			head := testsuite.MustSucceed(node.Beacon.Head(ctx))
			exit := testsuite.MustSucceed(validatorops.VoluntaryExit(key, validator.Index, chain.Epoch(head.Slot), chain))
			gomega.Expect(node.Beacon.SubmitVoluntaryExit(ctx, exit)).To(gomega.Succeed())

			ginkgo.By("waiting for the exit to be included and the stake to be withdrawn")
			scanner := newOperationScanner(node.Beacon, head.Slot)
			var exitIncluded bool
			var withdrawn *beacon.Withdrawal
			gomega.Eventually(func(g gomega.Gomega) {
				g.Expect(scanner.scan(ctx, func(operations beacon.BlockOperations) {
					for _, index := range operations.VoluntaryExits {
						exitIncluded = exitIncluded || index == validator.Index
					}
					for _, withdrawal := range operations.Withdrawals {
						if withdrawal.ValidatorIndex == validator.Index {
							withdrawn = &withdrawal
						}
					}
				})).To(gomega.Succeed())
				g.Expect(exitIncluded).To(gomega.BeTrue(), "exit not yet included")
				g.Expect(withdrawn).NotTo(gomega.BeNil(), "stake not yet withdrawn")
			}).WithContext(ctx).WithTimeout(exitTimeout).WithPolling(pollInterval).Should(gomega.Succeed())

			gomega.Expect(strings.EqualFold(withdrawn.Address, recipientHex)).To(gomega.BeTrue(),
				"withdrawal went to %s, not the staker wallet", withdrawn.Address)
			gomega.Expect(withdrawn.Amount).To(gomega.BeNumerically(">", 0))

			ginkgo.By("checking the validator record and the execution balance")
			withdrawnValue := new(big.Int).Mul(new(big.Int).SetUint64(withdrawn.Amount), big.NewInt(params.Shor))
			gomega.Eventually(func(g gomega.Gomega) {
				record, err := node.Beacon.Validator(ctx, publicKey)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(record.Status).To(gomega.Equal("withdrawal_done"))
				g.Expect(record.Balance).To(gomega.BeZero())
				balanceAfter, err := node.Execution.BalanceAt(ctx, recipient, nil)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				gained := new(big.Int).Sub(balanceAfter, balanceBefore)
				g.Expect(gained.Cmp(withdrawnValue)).To(gomega.BeZero(),
					"staker wallet gained %s planck, expected exactly %s planck", gained, withdrawnValue)
				validator = record
			}).WithContext(ctx).WithTimeout(exitTimeout).WithPolling(pollInterval).Should(gomega.Succeed())
			gomega.Expect(validator.ExitEpoch).NotTo(gomega.Equal(beacon.FarFutureEpoch))
			gomega.Expect(validator.WithdrawableEpoch).To(gomega.BeNumerically(">", validator.ExitEpoch))
		}, ginkgo.SpecTimeout(exitTimeout))
	},
)

// operationScanner walks every block after a starting slot exactly once, so
// polling callers do not miss operations between checks.
type operationScanner struct {
	client   *beacon.Client
	lastSlot uint64
}

func newOperationScanner(client *beacon.Client, lastSlot uint64) *operationScanner {
	return &operationScanner{client: client, lastSlot: lastSlot}
}

func (scanner *operationScanner) scan(ctx ginkgo.SpecContext, visit func(beacon.BlockOperations)) error {
	head, err := scanner.client.HeadSlot(ctx)
	if err != nil {
		return err
	}
	for slot := scanner.lastSlot + 1; slot <= head; slot++ {
		operations, err := scanner.client.BlockOperations(ctx, strconv.FormatUint(slot, 10))
		if beacon.IsNotFound(err) {
			scanner.lastSlot = slot
			continue
		}
		if err != nil {
			return err
		}
		visit(operations)
		scanner.lastSlot = slot
	}
	return nil
}
