// Package staking holds the validator lifecycle checks the consensus staking
// suites share once each has made its deposit.
package staking

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/params"
)

const (
	PollInterval = 2 * time.Second

	// Activation waits for eligibility finalization and the seed lookahead.
	// Attestation rewards are only served after two later epochs.
	activationQueueTimeout = 8 * time.Minute
	attestationTimeout     = 4 * time.Minute

	// ActivationTimeout bounds ExpectActiveAndAttesting.
	ActivationTimeout = activationQueueTimeout + attestationTimeout
	// ExitTimeout bounds ExpectExitAndWithdrawal: two committee epochs, the
	// five-epoch exit lookahead, and two withdrawability epochs take about six
	// minutes on a healthy network.
	ExitTimeout = 8 * time.Minute
)

// ExpectActiveAndAttesting waits for the validator to activate and earn an
// attestation reward, and returns its record.
func ExpectActiveAndAttesting(
	ctx context.Context,
	node *live.Node,
	chain chaininfo.Info,
	publicKey string,
) beacon.Validator {
	var validator beacon.Validator
	ginkgo.By("waiting for the activation queue")
	gomega.Eventually(func(g gomega.Gomega) {
		record, err := node.Beacon.Validator(ctx, publicKey)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(record.Status).To(gomega.Equal("active_ongoing"))
		validator = record
	}).WithContext(ctx).
		WithTimeout(activationQueueTimeout).
		WithPolling(PollInterval).
		Should(gomega.Succeed())

	// Every active validator attests once per epoch, so a missed attestation
	// can be made up in a later epoch.
	ginkgo.By("waiting for the validator client to attest")
	next := validator.ActivationEpoch
	gomega.Eventually(func(g gomega.Gomega) {
		var attested bool
		var err error
		next, attested, err = attestedFrom(ctx, node.Beacon, chain, validator.Index, next)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(attested).To(gomega.BeTrue(),
			"validator client produced no attestation reward since epoch %d", validator.ActivationEpoch)
	}).WithContext(ctx).
		WithTimeout(attestationTimeout).
		WithPolling(PollInterval).
		Should(gomega.Succeed())
	return validator
}

// ExpectExitAndWithdrawal waits out the committee period, submits the exit
// through submitExit, and waits until the stake has been withdrawn to
// recipient. It returns the final validator record.
func ExpectExitAndWithdrawal(
	ctx context.Context,
	node *live.Node,
	chain chaininfo.Info,
	validator beacon.Validator,
	recipient common.Address,
	submitExit func(ctx context.Context, epoch uint64) error,
) beacon.Validator {
	gomega.Expect(validator.Status).To(gomega.Equal("active_ongoing"), "the validator must be active")
	committeePeriod := testsuite.MustSucceed(node.Beacon.SpecUint(ctx, "SHARD_COMMITTEE_PERIOD"))

	ginkgo.By("waiting until the validator has been active for the committee period")
	gomega.Eventually(func(g gomega.Gomega) {
		head, err := node.Beacon.HeadSlot(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(chain.Epoch(head)).To(gomega.BeNumerically(">=", validator.ActivationEpoch+committeePeriod))
	}).WithContext(ctx).
		WithTimeout(ExitTimeout).
		WithPolling(PollInterval).
		Should(gomega.Succeed())

	ginkgo.By("submitting the voluntary exit")
	// Reward sweeps already pay the recipient before the exit, so compare the
	// wallet's gain with the withdrawals after one fixed block: the baseline is
	// read at the head block's execution block, and the scan starts after it.
	head := testsuite.MustSucceed(node.Beacon.BlockOperations(ctx, "head"))
	headBlock := new(big.Int).SetUint64(head.ExecutionBlockNumber)
	balanceBefore := testsuite.MustSucceed(node.Execution.BalanceAt(ctx, recipient, headBlock))
	gomega.Expect(submitExit(ctx, chain.Epoch(head.Slot))).To(gomega.Succeed())

	ginkgo.By("waiting for the exit to be included and the stake to be withdrawn")
	scanner := &operationScanner{
		client:    node.Beacon,
		lastSlot:  head.Slot,
		lastBlock: head.ExecutionBlockNumber,
	}
	recipientHex := hexutil.Encode(recipient[:])
	var exitIncluded bool
	var withdrawals []beacon.Withdrawal
	gomega.Eventually(func(g gomega.Gomega) {
		g.Expect(scanner.scan(ctx, func(operations beacon.BlockOperations) {
			for _, index := range operations.VoluntaryExits {
				exitIncluded = exitIncluded || index == validator.Index
			}
			for _, withdrawal := range operations.Withdrawals {
				if withdrawal.ValidatorIndex == validator.Index {
					withdrawals = append(withdrawals, withdrawal)
				}
			}
		})).To(gomega.Succeed())
		g.Expect(exitIncluded).To(gomega.BeTrue(), "exit not yet included")

		var withdrawnShor uint64
		for _, withdrawal := range withdrawals {
			g.Expect(strings.EqualFold(withdrawal.Address, recipientHex)).To(gomega.BeTrue(),
				"withdrawal went to %s, not %s", withdrawal.Address, recipientHex)
			withdrawnShor += withdrawal.Amount
		}

		// An exited validator can stay in the sync committee for up to two
		// periods, earning rewards that later sweeps pay out, so its balance
		// need not stay at zero. withdrawal_done means its effective balance is
		// zero, and the withdrawals must also cover the stake.
		record, err := node.Beacon.Validator(ctx, validator.PublicKey)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(record.Status).To(gomega.Equal("withdrawal_done"))
		g.Expect(withdrawnShor).To(gomega.BeNumerically(">=", validator.EffectiveBalance),
			"withdrawals do not cover the stake")

		// Rewards keep reaching the recipient, so read the wallet at the last
		// scanned block, whose withdrawals are all counted.
		lastBlock := new(big.Int).SetUint64(scanner.lastBlock)
		balanceAfter, err := node.Execution.BalanceAt(ctx, recipient, lastBlock)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		gained := new(big.Int).Sub(balanceAfter, balanceBefore)
		withdrawn := new(big.Int).Mul(new(big.Int).SetUint64(withdrawnShor), big.NewInt(params.Shor))
		g.Expect(gained.Cmp(withdrawn)).To(gomega.BeZero(),
			"recipient gained %s planck from %d withdrawals, expected %s planck",
			gained, len(withdrawals), withdrawn)
		validator = record
	}).WithContext(ctx).
		WithTimeout(ExitTimeout).
		WithPolling(PollInterval).
		Should(gomega.Succeed())
	return validator
}

// attestedFrom checks the validator's attestation rewards from epoch from
// through the latest epoch whose rewards are served, two before the head. It
// reports whether one was positive, and the epoch to check next.
func attestedFrom(
	ctx context.Context,
	client *beacon.Client,
	chain chaininfo.Info,
	index, from uint64,
) (uint64, bool, error) {
	head, err := client.HeadSlot(ctx)
	if err != nil {
		return from, false, err
	}
	epoch := from
	for ; epoch+2 <= chain.Epoch(head); epoch++ {
		rewards, err := client.AttestationRewards(ctx, epoch, []uint64{index})
		if err != nil {
			return epoch, false, err
		}
		if len(rewards) != 1 || rewards[0].ValidatorIndex != index {
			return epoch, false, fmt.Errorf("epoch %d rewards do not cover validator %d", epoch, index)
		}
		if rewards[0].Head > 0 || rewards[0].Target > 0 || rewards[0].Source > 0 {
			return epoch, true, nil
		}
	}
	return epoch, false, nil
}

// operationScanner walks every block after a starting slot exactly once, so
// polling callers do not miss operations between checks. lastBlock is the
// execution block of the last block it visited.
type operationScanner struct {
	client    *beacon.Client
	lastSlot  uint64
	lastBlock uint64
}

func (scanner *operationScanner) scan(ctx context.Context, visit func(beacon.BlockOperations)) error {
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
		scanner.lastBlock = operations.ExecutionBlockNumber
	}
	return nil
}
