//go:build e2e

package slashing

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/deposit"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/validator"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const (
	doubleVoteAttempts = 3

	attestTimeout     = 5 * time.Minute
	doubleVoteTimeout = 15 * time.Minute
	exitTimeout       = 5 * time.Minute

	afterExitEpochs  = 2
	afterExitTimeout = 4 * time.Minute
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Slashing E2E suite")
}

// The suite follows the slashed validator through its exit and a few epochs
// after it. Its correlation penalty and withdrawal come half and all of the
// slashings vector after the slashing: 256 and 512 epochs, hours on the
// devnet's mainnet preset. Qrysm's unit tests of process_slashings cover the
// penalty, and the staking suites the withdrawal sweep; the withdrawable epoch
// both fall due by is checked here. Following them live is left for a lane on
// images built with the minimal preset, whose 64-epoch vector brings them
// within an hour.
var _ = ginkgo.Describe(
	"A validator whose key runs on two nodes at once",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "consensus", "slashing"),
	func() {
		var (
			// primary runs the validator from the start. duplicate is the
			// second copy of its key, on the node whose beacon node the suite
			// pauses: an operator's backup machine brought up while the
			// first still runs, with a slashing-protection database of its
			// own.
			primary, duplicate         *node
			val                        rewards.Validator
			spec                       slashingSpec
			keystores                  []sidecar.File
			pausedNodeValidators       []uint64
			slashingSlot, firstVote    uint64
			beforeSlashing, onSlashing beacon.Validator
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime := testsuite.LoadRuntime()
			nodes := testsuite.MustSucceed(runtime.Nodes(ctx))
			gomega.Expect(nodes).To(gomega.HaveLen(2), "the suite runs the validator on two nodes")
			for _, node := range nodes {
				gomega.Expect(node.BeaconGRPC).NotTo(gomega.BeEmpty(), "beacon gRPC is not published")
			}
			gomega.Expect(runtime.ValidatorImage).NotTo(gomega.BeEmpty(), "validator image is not configured")
			gomega.Expect(runtime.QrysmAlltoolsImage).NotTo(gomega.BeEmpty(), "Qrysm all-tools image is not configured")
			primary = &node{Node: nodes[0], graffiti: "primary"}
			duplicate = &node{Node: nodes[1], graffiti: "duplicate"}
			pausedNodeValidators = runtime.Profile.ParticipantValidators(1)

			spares := runtime.Profile.SpareValidators()
			gomega.Expect(spares).NotTo(gomega.BeEmpty(), "profile %q has no genesis validator that no client runs", runtime.Profile)
			beaconClient := primary.Beacon
			val = rewards.Validator{
				Client: beaconClient,
				Chain:  testsuite.MustSucceed(chaininfo.Load(ctx, beaconClient)),
				Spec:   testsuite.MustSucceed(rewards.LoadSpec(ctx, beaconClient)),
				Index:  spares[0],
			}
			spec = testsuite.MustSucceed(loadSlashingSpec(ctx, beaconClient))

			derived := testsuite.MustSucceed(deposit.GenesisKey(ctx, deposit.GenesisKeyConfig{
				Image:               runtime.QrysmAlltoolsImage,
				Mnemonic:            devnet.GenesisMnemonic,
				Index:               val.Index,
				WithdrawalRecipient: primary.Address.Hex(),
				Password:            validatorops.KeystorePassword,
			}))
			keystores = derived.Keystores

			record := testsuite.MustSucceed(beaconClient.Validator(ctx, strconv.FormatUint(val.Index, 10)))
			gomega.Expect(strings.EqualFold(record.PublicKey, derived.PublicKey)).To(gomega.BeTrue(),
				"genesis validator %d is not the key derived for its index", val.Index)
			gomega.Expect(record.Status).To(gomega.Equal("active_ongoing"))
		}, ginkgo.NodeTimeout(3*time.Minute))

		ginkgo.AfterEach(func() {
			for _, node := range []*node{primary, duplicate} {
				if node != nil {
					testsuite.ReportLogsOnFailure(node.graffiti+" validator client", node.client)
				}
			}
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(primary.stopClient()).To(gomega.Succeed())
			gomega.Expect(duplicate.stopClient()).To(gomega.Succeed())
		})

		ginkgo.It("attests through its validator client", func(ctx ginkgo.SpecContext) {
			head := testsuite.MustSucceed(primary.Beacon.HeadSlot(ctx))
			primary.startClient(ctx, keystores)
			val.FirstAttestedEpoch(ctx, val.Chain.Epoch(head), attestTimeout)
		}, ginkgo.SpecTimeout(attestTimeout+time.Minute))

		ginkgo.It("is slashed for a double vote once a second client runs its key on a node that sees a block late",
			func(ctx ginkgo.SpecContext) {
				scenario := doubleVote{
					val:                  val,
					primary:              primary,
					duplicate:            duplicate,
					pausedNodeValidators: pausedNodeValidators,
					keystores:            keystores,
				}
				for attempt := 1; ; attempt++ {
					gomega.Expect(attempt).To(gomega.BeNumerically("<=", doubleVoteAttempts),
						"no double vote was slashed after %d delayed blocks", doubleVoteAttempts)
					slot, run := scenario.attempt(ctx)
					if firstVote == 0 {
						firstVote = slot
					}
					if run && scenario.awaitSlashed(ctx) {
						break
					}
				}

				ginkgo.By("finding the evidence a block included")
				slot, evidence := scenario.findEvidence(ctx, firstVote)
				gomega.Expect(evidence.proposer).To(gomega.BeNil(),
					"the block at slot %d slashed the validator for a double proposal", slot)
				gomega.Expect(evidence.attester).NotTo(gomega.BeNil())
				first, second := evidence.attester.Attestation1.Data, evidence.attester.Attestation2.Data
				gomega.Expect(isDoubleVote(first, second)).To(gomega.BeTrue(),
					"the attestations of slots %d and %d are not a double vote", first.Slot, second.Slot)
				ginkgo.GinkgoWriter.Printf("slot %d included a double vote of slot %d for heads %s and %s\n",
					slot, first.Slot, first.BeaconBlockRoot, second.BeaconBlockRoot)
				slashingSlot = slot
			}, ginkgo.SpecTimeout(doubleVoteTimeout))

		ginkgo.It("pays the slashing penalty in the block that includes the evidence, and its proposer the whistleblower reward",
			func(ctx ginkgo.SpecContext) {
				index := strconv.FormatUint(val.Index, 10)
				beforeSlashing = testsuite.MustSucceed(val.Client.ValidatorAt(ctx, strconv.FormatUint(slashingSlot-1, 10), index))
				onSlashing = testsuite.MustSucceed(val.Client.ValidatorAt(ctx, strconv.FormatUint(slashingSlot, 10), index))
				gomega.Expect(beforeSlashing.Slashed).To(gomega.BeFalse())
				gomega.Expect(onSlashing.Slashed).To(gomega.BeTrue())
				gomega.Expect(onSlashing.Status).To(gomega.Equal("active_slashed"))

				// The block's own duties change the balance too. A block at
				// the first slot of an epoch also follows the processing that
				// applies the rewards of the epoch two before.
				epoch := val.Chain.Epoch(slashingSlot)
				penalty := spec.initialPenalty(beforeSlashing.EffectiveBalance)
				blocks := testsuite.MustSucceed(val.BlockBalanceChange(ctx, slashingSlot))
				var processed int64
				if slashingSlot%val.Chain.SlotsPerEpoch == 0 {
					reward := testsuite.MustSucceed(val.AttestationReward(ctx, epoch-2))
					processed = reward.Head + reward.Source + reward.Target
				}
				want := int64(beforeSlashing.Balance) - int64(penalty) + blocks + processed
				gomega.Expect(int64(onSlashing.Balance)).To(gomega.Equal(want),
					"balance went from %d to %d at slot %d: the %d shor penalty, %d shor from the block and %d from epoch processing make %d",
					beforeSlashing.Balance, onSlashing.Balance, slashingSlot, penalty, blocks, processed, want)

				exit := spec.exitEpoch(epoch)
				gomega.Expect(onSlashing.ExitEpoch).To(gomega.Equal(exit), "exit epoch of a validator slashed in epoch %d", epoch)
				gomega.Expect(onSlashing.WithdrawableEpoch).To(gomega.Equal(spec.withdrawableEpoch(epoch, exit)),
					"withdrawable epoch of a validator slashed in epoch %d", epoch)

				blockRewards := testsuite.MustSucceed(val.Client.BlockRewards(ctx, strconv.FormatUint(slashingSlot, 10)))
				gomega.Expect(blockRewards.AttesterSlashings).To(gomega.Equal(spec.whistleblowerReward(beforeSlashing.EffectiveBalance)),
					"what the block's proposer earned for the evidence")
				gomega.Expect(blockRewards.ProposerSlashings).To(gomega.BeZero())
			}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("loses exactly the missed-vote penalty in every epoch from its slashing through its exit",
			func(ctx ginkgo.SpecContext) {
				// Its votes no longer count, whatever its clients sign.
				exit := onSlashing.ExitEpoch
				val.AwaitRewards(ctx, exit)
				for epoch := val.Chain.Epoch(slashingSlot) + 1; epoch <= exit; epoch++ {
					expectMissedVotePenalty(ctx, val, epoch)
				}

				exitState := strconv.FormatUint(exit*val.Chain.SlotsPerEpoch, 10)
				exited := testsuite.MustSucceed(val.Client.ValidatorAt(ctx, exitState, strconv.FormatUint(val.Index, 10)))
				gomega.Expect(exited.Status).To(gomega.Equal("exited_slashed"), "status at the start of epoch %d", exit)
			}, ginkgo.SpecTimeout(exitTimeout))

		ginkgo.It("keeps losing the missed-vote penalty after its exit, with its stake locked until it can withdraw",
			func(ctx ginkgo.SpecContext) {
				// An exited validator has no duties, but a slashed one stays
				// liable for the votes it would have owed until its
				// withdrawable epoch.
				first := onSlashing.ExitEpoch + 1
				last := onSlashing.ExitEpoch + afterExitEpochs
				val.AwaitRewards(ctx, last)
				index := strconv.FormatUint(val.Index, 10)
				for epoch := first; epoch <= last; epoch++ {
					expectMissedVotePenalty(ctx, val, epoch)

					state := strconv.FormatUint((epoch+2)*val.Chain.SlotsPerEpoch, 10)
					record := testsuite.MustSucceed(val.Client.ValidatorAt(ctx, state, index))
					gomega.Expect(record.Status).To(gomega.Equal("exited_slashed"), "status at the start of epoch %d", epoch+2)
					gomega.Expect(record.WithdrawableEpoch).To(gomega.Equal(onSlashing.WithdrawableEpoch),
						"withdrawable epoch at the start of epoch %d", epoch+2)
					gomega.Expect(record.Balance).To(gomega.BeNumerically(">", 0), "balance at the start of epoch %d", epoch+2)
				}
			}, ginkgo.SpecTimeout(afterExitTimeout))

		ginkgo.It("slashes no other validator, and the chain keeps finalizing", func(ctx ginkgo.SpecContext) {
			slashed := testsuite.MustSucceed(val.Client.SlashedValidators(ctx))
			indices := make([]uint64, len(slashed))
			for position, record := range slashed {
				indices[position] = record.Index
			}
			gomega.Expect(indices).To(gomega.Equal([]uint64{val.Index}))

			// A healthy chain finalizes the epoch before the previous one.
			head := testsuite.MustSucceed(val.Client.HeadSlot(ctx))
			checkpoints := testsuite.MustSucceed(val.Client.FinalityCheckpoints(ctx))
			gomega.Expect(checkpoints.Finalized.Epoch+3).To(gomega.BeNumerically(">=", val.Chain.Epoch(head)),
				"finalized epoch %d at head slot %d", checkpoints.Finalized.Epoch, head)
		}, ginkgo.SpecTimeout(time.Minute))
	},
)

// node is a participant that can run a validator client of the suite's own.
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

// stopClient removes the client, printing its last lines first: removing the
// container loses its log, and the spec that stopped it may still fail.
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

// expectMissedVotePenalty checks that the validator's votes of epoch earned
// nothing and cost exactly the missed source and target penalties, and that
// its balance moved by that and by its block duties alone when those rewards
// were applied.
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
