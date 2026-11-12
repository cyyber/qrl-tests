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

// leakSuite is what the suite's specs share, in order.
type leakSuite struct {
	runtime *live.Runtime
	nodes   []*live.Node
	// beacon is the online participant's beacon node.
	beacon *beacon.Client
	chain  chaininfo.Info

	onlineValidator, offlineValidator rewards.Validator
	onlineIndices, offlineIndices     []uint64
	states                            beaconStates
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
	suite.beacon = suite.nodes[onlineParticipant].Beacon
	suite.chain = testsuite.MustSucceed(chaininfo.Load(ctx, suite.beacon))
	testsuite.ExpectShortEpochs(suite.chain)
	spec := testsuite.MustSucceed(rewards.LoadSpec(ctx, suite.beacon))

	profile := suite.runtime.Profile
	suite.onlineIndices = profile.ParticipantValidators(onlineParticipant)
	suite.offlineIndices = profile.ParticipantValidators(offlineParticipant)
	gomega.Expect(suite.onlineIndices).NotTo(gomega.BeEmpty(), "profile %q runs no validator on participant %d", profile, onlineParticipant+1)
	gomega.Expect(suite.offlineIndices).NotTo(gomega.BeEmpty(), "profile %q runs no validator on participant %d", profile, offlineParticipant+1)
	suite.onlineValidator = rewards.Validator{Client: suite.beacon, Chain: suite.chain, Spec: spec, Index: suite.onlineIndices[0]}
	suite.offlineValidator = rewards.Validator{Client: suite.beacon, Chain: suite.chain, Spec: spec, Index: suite.offlineIndices[0]}
	suite.states = beaconStates{client: suite.beacon, chain: suite.chain, online: suite.onlineIndices, offline: suite.offlineIndices}
	suite.balanceIncrement = testsuite.MustSucceed(suite.beacon.SpecUint(ctx, "EFFECTIVE_BALANCE_INCREMENT"))

	total := testsuite.MustSucceed(suite.beacon.TotalActiveBalance(ctx, "head"))
	stake := suite.effectiveStake(ctx, suite.offlineIndices)
	// More than a third goes offline, so the rest can't justify.
	gomega.Expect(3*stake > total).To(gomega.BeTrue(),
		"participant %d runs %d of the %d shor staked, not more than a third", offlineParticipant+1, stake, total)

	node := suite.nodes[offlineParticipant]
	gomega.Expect(node.ValidatorImage).NotTo(gomega.BeEmpty(), "validator image is not configured")
	gomega.Expect(node.QrysmAlltoolsImage).NotTo(gomega.BeEmpty(), "Qrysm all-tools image is not configured")
	gomega.Expect(node.BeaconGRPC).NotTo(gomega.BeEmpty(), "beacon gRPC is not published")
	suite.returningKeystores = testsuite.MustSucceed(validatorops.GenesisKeystores(
		ctx, suite.beacon, node.QrysmAlltoolsImage, node.Address.Hex(), suite.offlineValidator.Index))
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
	head, err := suite.beacon.HeadSlot(ctx)
	return suite.chain.Epoch(head), err
}

func (suite *leakSuite) currentEpoch(ctx context.Context) uint64 {
	ginkgo.GinkgoHelper()
	head := testsuite.MustSucceed(suite.beacon.HeadSlot(ctx))
	return max(suite.chain.EpochAt(time.Now()), suite.chain.Epoch(head))
}

// stopOfflineClient stops the offline participant's validator client, and
// checks that neither the finalized nor the justified checkpoint moves from
// epoch settled on, once the votes cast before the stop are processed.
func (suite *leakSuite) stopOfflineClient(ctx context.Context) (stopped, settled uint64, frozen beacon.FinalityCheckpoints) {
	ginkgo.GinkgoHelper()
	gomega.Expect(suite.runtime.StopParticipantValidators(ctx, offlineParticipant)).To(gomega.Succeed())
	stopped = suite.currentEpoch(ctx)

	settled = stopped + settleEpochs
	last := settled + stalledEpochs
	gomega.Eventually(suite.headEpoch).WithContext(ctx).
		WithTimeout((settleEpochs+stalledEpochs+1)*suite.chain.EpochDuration).
		WithPolling(suite.chain.SlotDuration).
		Should(gomega.BeNumerically(">=", last), "the head block has stopped advancing")

	frozen = suite.states.checkpointsAt(ctx, settled)
	for epoch := settled + 1; epoch <= last; epoch++ {
		checkpoints := suite.states.checkpointsAt(ctx, epoch)
		gomega.Expect(checkpoints.Finalized).To(gomega.Equal(frozen.Finalized),
			"finalized checkpoint at the start of epoch %d, with the validator client stopped since epoch %d", epoch, stopped)
		gomega.Expect(checkpoints.CurrentJustified).To(gomega.Equal(frozen.CurrentJustified),
			"justified checkpoint at the start of epoch %d, with the validator client stopped since epoch %d", epoch, stopped)
	}
	return stopped, settled, frozen
}

func (suite *leakSuite) effectiveStake(ctx context.Context, indices []uint64) uint64 {
	ginkgo.GinkgoHelper()
	return totalEffectiveBalance(testsuite.MustSucceed(suite.beacon.ValidatorsAt(ctx, "head", indices)))
}

// expectCleanFinality checks that no validator is slashed, and that every
// node finalizes the same checkpoint, at epoch since or later.
func (suite *leakSuite) expectCleanFinality(ctx context.Context, since uint64) {
	ginkgo.GinkgoHelper()
	for index, node := range suite.nodes {
		gomega.Expect(testsuite.MustSucceed(node.Beacon.SlashedValidators(ctx))).To(gomega.BeEmpty(),
			"validators slashed according to participant %d", index+1)
	}
	// Nodes can cross an epoch boundary a moment apart.
	gomega.Eventually(func(g gomega.Gomega, ctx context.Context) {
		want, err := suite.beacon.FinalityCheckpoints(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(want.Finalized.Epoch).To(gomega.BeNumerically(">=", since))
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

// beaconStates reads the online and the offline validators, and finality,
// from one node's beacon states.
type beaconStates struct {
	client          *beacon.Client
	chain           chaininfo.Info
	online, offline []uint64
}

func (states beaconStates) stateAt(epoch uint64) string {
	return strconv.FormatUint(epoch*states.chain.SlotsPerEpoch, 10)
}

// checkpointsAt reads the finality checkpoints at the start of epoch.
func (states beaconStates) checkpointsAt(ctx context.Context, epoch uint64) beacon.FinalityCheckpoints {
	ginkgo.GinkgoHelper()
	return testsuite.MustSucceed(states.client.FinalityCheckpointsAt(ctx, states.stateAt(epoch)))
}

// at reads the online validators' effective balance, and the offline
// validators, at the start of epoch.
func (states beaconStates) at(ctx context.Context, epoch uint64) (uint64, []beacon.Validator) {
	ginkgo.GinkgoHelper()
	online := totalEffectiveBalance(testsuite.MustSucceed(states.client.ValidatorsAt(ctx, states.stateAt(epoch), states.online)))
	return online, testsuite.MustSucceed(states.client.ValidatorsAt(ctx, states.stateAt(epoch), states.offline))
}

func totalEffectiveBalance(records []beacon.Validator) uint64 {
	var stake uint64
	for _, record := range records {
		stake += record.EffectiveBalance
	}
	return stake
}
