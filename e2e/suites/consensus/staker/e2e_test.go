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
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/params"
)

const (
	pollInterval = 2 * time.Second

	// Each phase waits on several epochs of the development network. The
	// deposit phase is bounded by execution-data voting: Qrysm fixes the
	// execution block time at 60s, so with the profile's follow distance of 8
	// blocks the first vote that can see a post-genesis deposit opens 16
	// minutes after genesis, and the deposit lands in the state shortly after.
	// Activation then waits for the eligibility epoch to finalize plus the
	// seed lookahead, and the exit for the committee period, the exit queue
	// and the withdrawability delay.
	depositTimeout    = 30 * time.Minute
	activationTimeout = 15 * time.Minute
	exitTimeout       = 20 * time.Minute

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
			node      *live.Node
			chain     consensuscontext.Context
			depositor *validatorops.Depositor
			key       *validatorops.Key
			publicKey string
			maximum   uint64
			validator beacon.Validator
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			node = testsuite.MustSucceed(testsuite.LoadRuntime().PrimaryNode(ctx))
			chain = testsuite.MustSucceed(consensuscontext.Load(ctx, node.Consensus))
			depositor = testsuite.MustSucceed(validatorops.NewDepositor(ctx, node, chain))
			key = testsuite.MustSucceed(validatorops.DeterministicKey(stakerKeyMarker))
			publicKey = hexutil.Encode(key.PublicKey())
			maximum = testsuite.MustSucceed(node.Consensus.SpecUint(ctx, "MAX_EFFECTIVE_BALANCE"))

			_, err := node.Consensus.Validator(ctx, publicKey)
			gomega.Expect(beacon.IsNotFound(err)).To(gomega.BeTrue(), "staker key is already a validator: %v", err)
		})

		ginkgo.It("deposits half the maximum balance and tops it up to the maximum", func(ctx ginkgo.SpecContext) {
			first := maximum / 2
			second := maximum - first

			ginkgo.By("submitting the initial deposit")
			_, err := depositor.Deposit(ctx, key, first)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("submitting the top-up deposit")
			_, err = depositor.Deposit(ctx, key, second)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("waiting for the beacon chain to process both deposits")
			gomega.Eventually(func(g gomega.Gomega) {
				record, err := node.Consensus.Validator(ctx, publicKey)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(record.Balance).To(gomega.BeNumerically(">=", maximum))
				g.Expect(record.EffectiveBalance).To(gomega.Equal(maximum))
				validator = record
			}).WithContext(ctx).WithTimeout(depositTimeout).WithPolling(pollInterval).Should(gomega.Succeed())

			gomega.Expect(validator.PublicKey).To(gomega.Equal(publicKey))
			gomega.Expect(strings.EqualFold(validator.WithdrawalRecipient, executionAddress(node))).To(gomega.BeTrue(),
				"withdrawal recipient %s is not the development wallet", validator.WithdrawalRecipient)
			gomega.Expect(strings.EqualFold(validator.RandaoCommitment, hexutil.Encode(key.RandaoCommitment()))).To(gomega.BeTrue(),
				"validator record carries a different RANDAO commitment than the deposit")
		}, ginkgo.SpecTimeout(depositTimeout))

		ginkgo.It("activates the validator and schedules it for attestation duties", func(ctx ginkgo.SpecContext) {
			ginkgo.By("waiting for the activation queue")
			gomega.Eventually(func(g gomega.Gomega) {
				record, err := node.Consensus.Validator(ctx, publicKey)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(record.Status).To(gomega.Equal("active_ongoing"))
				validator = record
			}).WithContext(ctx).WithTimeout(activationTimeout).WithPolling(pollInterval).Should(gomega.Succeed())

			gomega.Expect(validator.ActivationEpoch).NotTo(gomega.Equal(beacon.FarFutureEpoch))
			gomega.Expect(validator.ExitEpoch).To(gomega.Equal(beacon.FarFutureEpoch))
			gomega.Expect(validator.Slashed).To(gomega.BeFalse())

			ginkgo.By("checking the validator is assigned attestation duties")
			head := testsuite.MustSucceed(node.Consensus.Head(ctx))
			duties := testsuite.MustSucceed(node.Consensus.AttesterDuties(ctx, chain.Epoch(head.Slot), []uint64{validator.Index}))
			gomega.Expect(duties).To(gomega.HaveLen(1))
			gomega.Expect(duties[0].ValidatorIndex).To(gomega.Equal(validator.Index))
			gomega.Expect(strings.EqualFold(duties[0].PublicKey, publicKey)).To(gomega.BeTrue())
		}, ginkgo.SpecTimeout(activationTimeout))

		ginkgo.It("exits the validator and withdraws its stake to the execution wallet", func(ctx ginkgo.SpecContext) {
			gomega.Expect(validator.Status).To(gomega.Equal("active_ongoing"), "the activation spec must pass first")
			committeePeriod := testsuite.MustSucceed(node.Consensus.SpecUint(ctx, "SHARD_COMMITTEE_PERIOD"))

			ginkgo.By("waiting until the validator has been active for the committee period")
			gomega.Eventually(func(g gomega.Gomega) {
				head, err := node.Consensus.HeadSlot(ctx)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(chain.Epoch(head)).To(gomega.BeNumerically(">=", validator.ActivationEpoch+committeePeriod))
			}).WithContext(ctx).WithTimeout(exitTimeout).WithPolling(pollInterval).Should(gomega.Succeed())

			ginkgo.By("submitting the voluntary exit")
			head := testsuite.MustSucceed(node.Consensus.Head(ctx))
			exit := testsuite.MustSucceed(validatorops.VoluntaryExit(key, validator.Index, chain.Epoch(head.Slot), chain))
			gomega.Expect(node.Consensus.SubmitVoluntaryExit(ctx, exit)).To(gomega.Succeed())
			balanceBefore := testsuite.MustSucceed(node.Execution.BalanceAt(ctx, node.Address, nil))

			ginkgo.By("waiting for the exit to be included and the stake to be withdrawn")
			scanner := newOperationScanner(node.Consensus, head.Slot)
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

			gomega.Expect(strings.EqualFold(withdrawn.Address, executionAddress(node))).To(gomega.BeTrue(),
				"withdrawal went to %s, not the development wallet", withdrawn.Address)
			gomega.Expect(withdrawn.Amount).To(gomega.BeNumerically(">", 0))

			ginkgo.By("checking the validator record and the execution balance")
			gomega.Eventually(func(g gomega.Gomega) {
				record, err := node.Consensus.Validator(ctx, publicKey)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(record.Status).To(gomega.Equal("withdrawal_done"))
				g.Expect(record.Balance).To(gomega.BeZero())
				validator = record
			}).WithContext(ctx).WithTimeout(exitTimeout).WithPolling(pollInterval).Should(gomega.Succeed())
			gomega.Expect(validator.ExitEpoch).NotTo(gomega.Equal(beacon.FarFutureEpoch))
			gomega.Expect(validator.WithdrawableEpoch).To(gomega.BeNumerically(">", validator.ExitEpoch))

			// The development wallet is also the withdrawal recipient of the
			// genesis validators, so it only ever gains; the full withdrawal
			// must account for at least the staker's amount.
			balanceAfter := testsuite.MustSucceed(node.Execution.BalanceAt(ctx, node.Address, nil))
			withdrawnValue := new(big.Int).Mul(new(big.Int).SetUint64(withdrawn.Amount), big.NewInt(params.Shor))
			gomega.Expect(new(big.Int).Sub(balanceAfter, balanceBefore)).To(gomega.BeNumerically(">=", withdrawnValue))
		}, ginkgo.SpecTimeout(exitTimeout))
	},
)

// executionAddress renders the development wallet address the way the beacon
// API renders 64-byte addresses: 0x-prefixed hex.
func executionAddress(node *live.Node) string {
	return hexutil.Encode(node.Address.Bytes())
}

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
