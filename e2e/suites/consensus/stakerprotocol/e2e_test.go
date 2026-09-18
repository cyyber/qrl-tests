//go:build e2e

package stakerprotocol

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/consensuscontext"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/operatorvc"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorclient"
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
	// Attestation rewards are only served after two later epochs.
	// Exit waits two committee epochs, the five-epoch exit lookahead, and
	// two withdrawability epochs: about six minutes on a healthy network.
	activationTimeout    = 8 * time.Minute
	dutyLookaheadTimeout = 2 * time.Minute
	attestationTimeout   = 4 * time.Minute
	exitTimeout          = 8 * time.Minute

	// stakerKeyMarker seeds the staker's validator key; it must not collide
	// with the genesis validators, which derive from the package mnemonic.
	stakerKeyMarker = 0x91
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Staker protocol lifecycle E2E suite")
}

var _ = ginkgo.Describe(
	"A new staker through the protocol APIs",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "consensus", "staker-protocol", "mutates-chain"),
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
			dutyEpoch    uint64
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			node = testsuite.MustSucceed(testsuite.LoadRuntime().PrimaryNode(ctx))
			gomega.Expect(node.ValidatorImage).NotTo(gomega.BeEmpty(), "validator image is not configured")
			gomega.Expect(node.BeaconGRPC).NotTo(gomega.BeEmpty(), "beacon gRPC is not published")
			operator := testsuite.MustSucceed(operatorvc.Start(ctx, operatorvc.Config{
				Image:              node.ValidatorImage,
				BeaconHTTPURL:      node.BeaconURL,
				BeaconGRPC:         node.BeaconGRPC,
				ConsensusServiceID: node.ConsensusServiceID,
			}))
			ginkgo.DeferCleanup(operator.Close)
			node.Validator = operator.Keymanager
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
		}, ginkgo.NodeTimeout(2*time.Minute))

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

			ginkgo.By("importing the staker key into the operator validator client")
			keystore := testsuite.MustSucceed(key.KeystoreJSON(validatorops.KeystorePassword))
			gomega.Expect(node.Validator.ImportKeystore(ctx, keystore, validatorops.KeystorePassword)).To(gomega.Succeed())
			keystores := testsuite.MustSucceed(node.Validator.ListKeystores(ctx))
			gomega.Expect(containsPublicKey(keystores, publicKey)).To(gomega.BeTrue(), "imported key is not in the validator client")

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

			ginkgo.By("waiting for a future attestation duty")
			gomega.Eventually(func(g gomega.Gomega) {
				duty, err := futureAttesterDuty(ctx, node.Beacon, chain, validator.Index, publicKey)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				dutyEpoch = chain.Epoch(duty.Slot)
			}).WithContext(ctx).WithTimeout(dutyLookaheadTimeout).WithPolling(pollInterval).Should(gomega.Succeed())

			ginkgo.By("waiting for the validator client to attest the assigned epoch")
			gomega.Eventually(func(g gomega.Gomega) {
				head, err := node.Beacon.HeadSlot(ctx)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(chain.Epoch(head)).To(gomega.BeNumerically(">=", dutyEpoch+2),
					"attestation rewards need two later epochs")
				rewards, err := node.Beacon.AttestationRewards(ctx, dutyEpoch, []uint64{validator.Index})
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(rewards).To(gomega.HaveLen(1))
				g.Expect(rewards[0].ValidatorIndex).To(gomega.Equal(validator.Index))
				g.Expect(rewards[0].Head > 0 || rewards[0].Target > 0 || rewards[0].Source > 0).To(gomega.BeTrue(),
					"validator client produced no attestation reward in epoch %d", dutyEpoch)
			}).WithContext(ctx).WithTimeout(attestationTimeout).WithPolling(pollInterval).Should(gomega.Succeed())
		}, ginkgo.SpecTimeout(activationTimeout+dutyLookaheadTimeout+attestationTimeout))

		ginkgo.It("exits the validator and withdraws its stake to the staker wallet", func(ctx ginkgo.SpecContext) {
			gomega.Expect(validator.Status).To(gomega.Equal("active_ongoing"), "the activation spec must pass first")
			committeePeriod := testsuite.MustSucceed(node.Beacon.SpecUint(ctx, "SHARD_COMMITTEE_PERIOD"))

			ginkgo.By("waiting until the validator has been active for the committee period")
			gomega.Eventually(func(g gomega.Gomega) {
				head, err := node.Beacon.HeadSlot(ctx)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(chain.Epoch(head)).To(gomega.BeNumerically(">=", validator.ActivationEpoch+committeePeriod))
			}).WithContext(ctx).WithTimeout(exitTimeout).WithPolling(pollInterval).Should(gomega.Succeed())

			ginkgo.By("signing the voluntary exit with the operator validator client")
			// Reward skims can already have credited this address, so the
			// later check uses a wallet delta against every withdrawal.
			balanceBefore := testsuite.MustSucceed(node.Execution.BalanceAt(ctx, recipient, nil))
			headSlot := testsuite.MustSucceed(node.Beacon.HeadSlot(ctx))
			exit := testsuite.MustSucceed(node.Validator.SignVoluntaryExit(ctx, publicKey, chain.Epoch(headSlot)))
			gomega.Expect(node.Beacon.SubmitVoluntaryExit(ctx, exit)).To(gomega.Succeed())

			ginkgo.By("waiting for the exit to be included and the full stake to be withdrawn")
			scanner := newOperationScanner(node.Beacon, headSlot)
			var exitIncluded bool
			var withdrawals []beacon.Withdrawal
			var fullWithdrawal *beacon.Withdrawal
			gomega.Eventually(func(g gomega.Gomega) {
				g.Expect(scanner.scan(ctx, func(operations beacon.BlockOperations) {
					for _, index := range operations.VoluntaryExits {
						exitIncluded = exitIncluded || index == validator.Index
					}
					for _, withdrawal := range operations.Withdrawals {
						if withdrawal.ValidatorIndex != validator.Index {
							continue
						}
						withdrawals = append(withdrawals, withdrawal)
						if withdrawal.Amount >= maximum {
							current := withdrawal
							fullWithdrawal = &current
						}
					}
				})).To(gomega.Succeed())
				g.Expect(exitIncluded).To(gomega.BeTrue(), "exit not yet included")
				g.Expect(fullWithdrawal).NotTo(gomega.BeNil(), "full stake not yet withdrawn")
			}).WithContext(ctx).WithTimeout(exitTimeout).WithPolling(pollInterval).Should(gomega.Succeed())

			for _, withdrawal := range withdrawals {
				gomega.Expect(strings.EqualFold(withdrawal.Address, recipientHex)).To(gomega.BeTrue(),
					"withdrawal went to %s, not the staker wallet", withdrawal.Address)
			}
			gomega.Expect(fullWithdrawal.Amount).To(gomega.BeNumerically(">=", maximum))

			ginkgo.By("checking the validator record and the execution balance")
			gomega.Eventually(func(g gomega.Gomega) {
				g.Expect(scanner.scan(ctx, func(operations beacon.BlockOperations) {
					for _, withdrawal := range operations.Withdrawals {
						if withdrawal.ValidatorIndex == validator.Index {
							withdrawals = append(withdrawals, withdrawal)
						}
					}
				})).To(gomega.Succeed())
				record, err := node.Beacon.Validator(ctx, publicKey)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(record.Status).To(gomega.Equal("withdrawal_done"))
				balanceAfter, err := node.Execution.BalanceAt(ctx, recipient, nil)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				gained := new(big.Int).Sub(balanceAfter, balanceBefore)
				withdrawnValue := withdrawalPlanck(withdrawals)
				g.Expect(gained.Cmp(withdrawnValue)).To(gomega.BeZero(),
					"staker wallet gained %s planck from %d withdrawals, expected %s planck",
					gained, len(withdrawals), withdrawnValue)
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

func futureAttesterDuty(ctx ginkgo.SpecContext, client *beacon.Client, chain consensuscontext.Context, index uint64, publicKey string) (beacon.AttesterDuty, error) {
	headSlot, err := client.HeadSlot(ctx)
	if err != nil {
		return beacon.AttesterDuty{}, err
	}
	epoch := chain.Epoch(headSlot)
	for _, candidate := range []uint64{epoch, epoch + 1} {
		duties, err := client.AttesterDuties(ctx, candidate, []uint64{index})
		if err != nil {
			if candidate == epoch {
				return beacon.AttesterDuty{}, err
			}
			continue
		}
		if len(duties) != 1 {
			continue
		}
		duty := duties[0]
		if duty.ValidatorIndex != index || !strings.EqualFold(duty.PublicKey, publicKey) {
			continue
		}
		if chain.Epoch(duty.Slot) != candidate || duty.Slot <= headSlot {
			continue
		}
		return duty, nil
	}
	return beacon.AttesterDuty{}, fmt.Errorf("no future attester duty at head slot %d", headSlot)
}

func containsPublicKey(keystores []validatorclient.Keystore, publicKey string) bool {
	wanted := strings.TrimPrefix(strings.ToLower(publicKey), "0x")
	for _, keystore := range keystores {
		if strings.TrimPrefix(strings.ToLower(keystore.PublicKey), "0x") == wanted {
			return true
		}
	}
	return false
}

func withdrawalPlanck(withdrawals []beacon.Withdrawal) *big.Int {
	total := new(big.Int)
	shor := big.NewInt(params.Shor)
	for _, withdrawal := range withdrawals {
		total.Add(total, new(big.Int).Mul(new(big.Int).SetUint64(withdrawal.Amount), shor))
	}
	return total
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
