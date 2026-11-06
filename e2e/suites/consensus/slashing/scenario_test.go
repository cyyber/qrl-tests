//go:build e2e

package slashing

import (
	"context"
	"strconv"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/validator"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// node is a devnet participant that runs one of the suite's validator sidecars.
type node struct {
	*live.Node
	graffiti string
	client   *validator.Sidecar
}

func (node *node) startClient(ctx context.Context, keystores []sidecar.File) {
	ginkgo.GinkgoHelper()
	node.client = testsuite.MustSucceed(validator.Start(ctx, validator.Config{
		Image:              node.ValidatorImage,
		BeaconURL:          node.BeaconURL,
		BeaconGRPC:         node.BeaconGRPC,
		ConsensusServiceID: node.ConsensusServiceID,
		Keystores:          keystores,
		Graffiti:           node.graffiti,
	}))
}

// stopClient removes the client, printing its last log lines first.
func (node *node) stopClient() error {
	if node == nil {
		return nil
	}
	client := node.client
	node.client = nil
	if client == nil {
		return nil
	}
	if logs, err := client.Logs(); err != nil {
		ginkgo.GinkgoWriter.Printf("%s validator client logs unavailable: %v\n", node.graffiti, err)
	} else {
		ginkgo.GinkgoWriter.Printf("%s validator client, last log lines:\n%s\n", node.graffiti, logs)
	}
	return client.Close()
}

// expectSlashedInBlock checks the block at slot that slashes the validator: it
// pays the initial penalty, sets the exit and withdrawable epochs, and pays its
// proposer the whistleblower reward for an attester slashing. It returns the
// validator's record at the block.
func expectSlashedInBlock(ctx context.Context, val rewards.Validator, spec slashingSpec, slot uint64) beacon.Validator {
	ginkgo.GinkgoHelper()
	index := strconv.FormatUint(val.Index, 10)
	before := testsuite.MustSucceed(val.Client.ValidatorAt(ctx, strconv.FormatUint(slot-1, 10), index))
	on := testsuite.MustSucceed(val.Client.ValidatorAt(ctx, strconv.FormatUint(slot, 10), index))
	gomega.Expect(before.Slashed).To(gomega.BeFalse())
	gomega.Expect(on.Slashed).To(gomega.BeTrue())
	gomega.Expect(on.Status).To(gomega.Equal("active_slashed"))

	// The block's own duties change the balance too, and a block at an
	// epoch's first slot also follows epoch processing.
	epoch := val.Chain.Epoch(slot)
	penalty := spec.initialPenalty(before.EffectiveBalance)
	blocks := testsuite.MustSucceed(val.BlockBalanceChange(ctx, slot))
	var processed int64
	if slot%val.Chain.SlotsPerEpoch == 0 {
		reward := testsuite.MustSucceed(val.AttestationReward(ctx, epoch-2))
		processed = reward.Head + reward.Source + reward.Target
	}
	want := int64(before.Balance) - int64(penalty) + blocks + processed
	gomega.Expect(int64(on.Balance)).To(gomega.Equal(want),
		"balance went from %d to %d at slot %d: the %d shor penalty, %d shor from the block and %d from epoch processing make %d",
		before.Balance, on.Balance, slot, penalty, blocks, processed, want)

	exit := spec.exitEpoch(epoch)
	gomega.Expect(on.ExitEpoch).To(gomega.Equal(exit), "exit epoch of a validator slashed in epoch %d", epoch)
	gomega.Expect(on.WithdrawableEpoch).To(gomega.Equal(spec.withdrawableEpoch(epoch, exit)),
		"withdrawable epoch of a validator slashed in epoch %d", epoch)

	blockRewards := testsuite.MustSucceed(val.Client.BlockRewards(ctx, strconv.FormatUint(slot, 10)))
	gomega.Expect(blockRewards.AttesterSlashings).To(gomega.Equal(spec.whistleblowerReward(before.EffectiveBalance)),
		"what the block's proposer earned for the evidence")
	gomega.Expect(blockRewards.ProposerSlashings).To(gomega.BeZero())
	return on
}

// expectMissedVotePenalty checks that the validator's votes of epoch cost
// exactly the missed source and target penalties, and that its balance moved
// by that and its block duties alone.
func expectMissedVotePenalty(ctx context.Context, validator rewards.Validator, epoch uint64) {
	ginkgo.GinkgoHelper()
	reward := testsuite.MustSucceed(validator.AttestationReward(ctx, epoch))
	amounts := testsuite.MustSucceed(validator.VoteAmounts(ctx, epoch))
	gomega.Expect(reward).To(gomega.Equal(beacon.AttestationReward{
		ValidatorIndex: validator.Index,
		Source:         -int64(amounts.MissedSource),
		Target:         -int64(amounts.MissedTarget),
	}), "attestation rewards of epoch %d", epoch)

	before := testsuite.MustSucceed(validator.BalanceAtEpochStart(ctx, epoch+1))
	after := testsuite.MustSucceed(validator.BalanceAtEpochStart(ctx, epoch+2))
	blocks := testsuite.MustSucceed(validator.BlockBalanceChanges(ctx, epoch+1))
	penalty := amounts.MissedSource + amounts.MissedTarget
	want := int64(before) - int64(penalty) + blocks
	gomega.Expect(int64(after)).To(gomega.Equal(want),
		"balance went from %d to %d over epoch %d: the %d shor penalty of epoch %d and %d shor from blocks make %d",
		before, after, epoch+1, penalty, epoch, blocks, want)
}
