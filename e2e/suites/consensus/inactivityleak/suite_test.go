//go:build e2e

package inactivityleak

import (
	"context"
	"strconv"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/validator"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const (
	onlineParticipant  = 0
	offlineParticipant = 1

	finalityTimeoutEpochs = 8
	settleEpochs          = 2
	stalledEpochs         = 4

	finalityTimeout = 7 * time.Minute
	stallTimeout    = 6 * time.Minute
	checkTimeout    = 2 * time.Minute
)

type leakSuite struct {
	runtime      *live.Runtime
	nodes        []*live.Node
	onlineBeacon *beacon.Client
	chain        chaininfo.Info

	onlineValidator, offlineValidator rewards.Validator
	onlineIndices, offlineIndices     []uint64
	balanceIncrement                  uint64

	returningKeystores []sidecar.File
	returnedClient     *validator.Sidecar

	returning returningOutage
	lasting   lastingOutage
}

func (suite *leakSuite) setUp(ctx ginkgo.SpecContext) {
	suite.runtime = testsuite.LoadRuntime()
	suite.nodes = testsuite.MustSucceed(suite.runtime.Nodes(ctx))
	gomega.Expect(suite.nodes).To(gomega.HaveLen(2), "the suite stops one of two participants' validator clients")
	suite.onlineBeacon = suite.nodes[onlineParticipant].Beacon
	suite.chain = testsuite.MustSucceed(chaininfo.Load(ctx, suite.onlineBeacon))
	testsuite.ExpectShortEpochs(suite.chain)
	spec := testsuite.MustSucceed(rewards.LoadSpec(ctx, suite.onlineBeacon))

	profile := suite.runtime.Profile
	suite.onlineIndices = profile.ParticipantValidators(onlineParticipant)
	suite.offlineIndices = profile.ParticipantValidators(offlineParticipant)
	gomega.Expect(suite.onlineIndices).NotTo(gomega.BeEmpty(), "profile %q runs no validator on participant %d", profile, onlineParticipant+1)
	gomega.Expect(suite.offlineIndices).NotTo(gomega.BeEmpty(), "profile %q runs no validator on participant %d", profile, offlineParticipant+1)
	suite.onlineValidator = rewards.Validator{Client: suite.onlineBeacon, Chain: suite.chain, Spec: spec, Index: suite.onlineIndices[0]}
	suite.offlineValidator = rewards.Validator{Client: suite.onlineBeacon, Chain: suite.chain, Spec: spec, Index: suite.offlineIndices[0]}
	suite.balanceIncrement = testsuite.MustSucceed(suite.onlineBeacon.SpecUint(ctx, "EFFECTIVE_BALANCE_INCREMENT"))

	total := testsuite.MustSucceed(suite.onlineBeacon.TotalActiveBalance(ctx, "head"))
	stake := totalEffectiveBalance(testsuite.MustSucceed(suite.onlineBeacon.ValidatorsAt(ctx, "head", suite.offlineIndices)))
	gomega.Expect(3*stake > total).To(gomega.BeTrue(),
		"participant %d runs %d of the %d shor staked, not more than a third", offlineParticipant+1, stake, total)

	node := suite.nodes[offlineParticipant]
	gomega.Expect(node.ValidatorImage).NotTo(gomega.BeEmpty(), "validator image is not configured")
	gomega.Expect(node.QrysmAlltoolsImage).NotTo(gomega.BeEmpty(), "Qrysm all-tools image is not configured")
	gomega.Expect(node.BeaconGRPC).NotTo(gomega.BeEmpty(), "beacon gRPC is not published")
	suite.returningKeystores = testsuite.MustSucceed(validatorops.GenesisKeystores(
		ctx, suite.onlineBeacon, node.QrysmAlltoolsImage, node.Address.Hex(), suite.offlineValidator.Index))
}

func (suite *leakSuite) reportReturnedLogs() {
	if suite.returnedClient != nil {
		testsuite.ReportLogsOnFailure("returned validator's client", suite.returnedClient)
	}
}

// closeReturned closes the returned validator's sidecar, if open. The lane
// stops the network but not the suite's own containers.
func (suite *leakSuite) closeReturned() {
	ginkgo.GinkgoHelper()
	if suite.returnedClient != nil {
		gomega.Expect(suite.returnedClient.Close()).To(gomega.Succeed())
		suite.returnedClient = nil
	}
}

func (suite *leakSuite) headEpoch(ctx context.Context) (uint64, error) {
	head, err := suite.onlineBeacon.HeadSlot(ctx)
	return suite.chain.Epoch(head), err
}

func (suite *leakSuite) currentEpoch(ctx context.Context) uint64 {
	ginkgo.GinkgoHelper()
	head := testsuite.MustSucceed(suite.onlineBeacon.HeadSlot(ctx))
	return max(suite.chain.EpochAt(time.Now()), suite.chain.Epoch(head))
}

// stopValidatorClient stops the second participant's validator client and
// returns the epoch it stopped in.
func (suite *leakSuite) stopValidatorClient(ctx context.Context) uint64 {
	ginkgo.GinkgoHelper()
	gomega.Expect(suite.runtime.StopParticipantValidators(ctx, offlineParticipant)).To(gomega.Succeed())
	return suite.currentEpoch(ctx)
}

// expectFrozenFinality checks that finality and justification freeze from
// epoch settled on, after the validator client stopped in epoch stopped.
func (suite *leakSuite) expectFrozenFinality(ctx context.Context, stopped uint64) (settled uint64, frozen beacon.FinalityCheckpoints) {
	ginkgo.GinkgoHelper()
	settled = stopped + settleEpochs
	last := settled + stalledEpochs
	gomega.Eventually(suite.headEpoch).WithContext(ctx).
		WithTimeout((settleEpochs+stalledEpochs+1)*suite.chain.EpochDuration).
		WithPolling(suite.chain.SlotDuration).
		Should(gomega.BeNumerically(">=", last), "the head block has stopped advancing")

	frozen = suite.checkpointsAtEpochStart(ctx, settled)
	for epoch := settled + 1; epoch <= last; epoch++ {
		checkpoints := suite.checkpointsAtEpochStart(ctx, epoch)
		gomega.Expect(checkpoints.Finalized).To(gomega.Equal(frozen.Finalized),
			"finalized checkpoint at the start of epoch %d, with the validator client stopped since epoch %d", epoch, stopped)
		gomega.Expect(checkpoints.CurrentJustified).To(gomega.Equal(frozen.CurrentJustified),
			"justified checkpoint at the start of epoch %d, with the validator client stopped since epoch %d", epoch, stopped)
	}
	return settled, frozen
}

// expectCleanFinality checks that no validator is slashed, and that every
// node finalizes the same checkpoint, no earlier than the given epoch.
func (suite *leakSuite) expectCleanFinality(ctx context.Context, epoch uint64) {
	ginkgo.GinkgoHelper()
	for index, node := range suite.nodes {
		gomega.Expect(testsuite.MustSucceed(node.Beacon.SlashedValidators(ctx))).To(gomega.BeEmpty(),
			"validators slashed according to participant %d", index+1)
	}
	// Nodes can cross an epoch boundary a moment apart.
	gomega.Eventually(func(g gomega.Gomega, ctx context.Context) {
		want, err := suite.onlineBeacon.FinalityCheckpoints(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(want.Finalized.Epoch).To(gomega.BeNumerically(">=", epoch))
		for index, node := range suite.nodes {
			got, err := node.Beacon.FinalityCheckpoints(ctx)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(got.Finalized).To(gomega.Equal(want.Finalized), "finalized checkpoint of participant %d", index+1)
		}
	}).WithContext(ctx).
		WithTimeout(suite.chain.EpochDuration).
		WithPolling(suite.chain.SlotDuration).
		Should(gomega.Succeed())
}

func (suite *leakSuite) stateAtEpochStart(epoch uint64) string {
	return strconv.FormatUint(epoch*suite.chain.SlotsPerEpoch, 10)
}

func (suite *leakSuite) checkpointsAtEpochStart(ctx context.Context, epoch uint64) beacon.FinalityCheckpoints {
	ginkgo.GinkgoHelper()
	return testsuite.MustSucceed(suite.onlineBeacon.FinalityCheckpointsAt(ctx, suite.stateAtEpochStart(epoch)))
}

func (suite *leakSuite) stakesAtEpochStart(ctx context.Context, epoch uint64) (onlineStake uint64, offline []beacon.Validator) {
	ginkgo.GinkgoHelper()
	state := suite.stateAtEpochStart(epoch)
	onlineStake = totalEffectiveBalance(testsuite.MustSucceed(suite.onlineBeacon.ValidatorsAt(ctx, state, suite.onlineIndices)))
	offline = testsuite.MustSucceed(suite.onlineBeacon.ValidatorsAt(ctx, state, suite.offlineIndices))
	return onlineStake, offline
}

func totalEffectiveBalance(records []beacon.Validator) uint64 {
	var stake uint64
	for _, record := range records {
		stake += record.EffectiveBalance
	}
	return stake
}
