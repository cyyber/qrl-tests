//go:build e2e

package protection

import (
	"strconv"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/interchange"
	"github.com/cyyber/qrl-tests/e2e/internal/keymanager"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/rewards"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/validator"
	"github.com/cyyber/qrl-tests/e2e/internal/slashingops"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const (
	aheadEpochs = 3

	// importTimeout covers the wait for a new chain's first justified epoch.
	importTimeout = 4 * time.Minute
	// refusalTimeout covers the imported epochs and the two epochs until
	// their rewards are served.
	refusalTimeout = 6 * time.Minute
	// resumeTimeout covers the epoch after the imported ones and the two
	// epochs until its rewards are served.
	resumeTimeout = 3 * time.Minute
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Slashing-protection import E2E suite")
}

// The imported history claims attestations its key never signed, so a
// conflicting vote would not be slashable on chain. The suite reads the
// refusal from the chain, which includes no vote of the validator for those
// targets, and from the history the client exports.
var _ = ginkgo.Describe(
	"A validator client that imports a key with its slashing-protection history",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.Label("e2e", "consensus", "slashing"),
	func() {
		var (
			node                  *live.Node
			val                   rewards.Validator
			keystore              string
			publicKey             string
			genesisValidatorsRoot string
			validatorClient       *validator.Sidecar
			// imported claims attestations for every target from first to
			// last.
			imported    interchange.Interchange
			first, last uint64
		)

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime := testsuite.LoadRuntime()
			node = testsuite.MustSucceed(runtime.PrimaryNode(ctx))
			gomega.Expect(node.ValidatorImage).NotTo(gomega.BeEmpty(), "validator image is not configured")
			gomega.Expect(node.QrysmAlltoolsImage).NotTo(gomega.BeEmpty(), "Qrysm all-tools image is not configured")
			gomega.Expect(node.BeaconGRPC).NotTo(gomega.BeEmpty(), "beacon gRPC is not published")

			spares := runtime.Profile.SpareValidators()
			gomega.Expect(spares).NotTo(gomega.BeEmpty(), "profile %q has no genesis validator that no client runs", runtime.Profile)
			val = rewards.Validator{
				Client: node.Beacon,
				Chain:  testsuite.MustSucceed(chaininfo.Load(ctx, node.Beacon)),
				Spec:   testsuite.MustSucceed(rewards.LoadSpec(ctx, node.Beacon)),
				Index:  spares[0],
			}
			slashingops.ExpectShortEpochs(val.Chain)

			keystores := testsuite.MustSucceed(validatorops.GenesisKeystores(
				ctx, node.Beacon, node.QrysmAlltoolsImage, node.Address.Hex(), val.Index))
			gomega.Expect(keystores).To(gomega.HaveLen(1))
			keystore = string(keystores[0].Body)
			publicKey = testsuite.MustSucceed(node.Beacon.Validator(ctx, strconv.FormatUint(val.Index, 10))).PublicKey
			genesisValidatorsRoot = testsuite.MustSucceed(node.Beacon.Genesis(ctx)).ValidatorsRoot

			// The client starts without keys, as the target of a migration.
			validatorClient = testsuite.MustSucceed(validator.Start(ctx, validator.Config{
				Image:              node.ValidatorImage,
				BeaconURL:          node.BeaconURL,
				BeaconGRPC:         node.BeaconGRPC,
				ConsensusServiceID: node.ConsensusServiceID,
			}))
		}, ginkgo.NodeTimeout(3*time.Minute))

		ginkgo.AfterEach(func() {
			testsuite.ReportLogsOnFailure("validator client", validatorClient)
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(validatorClient.Close()).To(gomega.Succeed())
		})

		ginkgo.It("imports the key through the keymanager API with a history that claims attestations a few epochs ahead",
			func(ctx ginkgo.SpecContext) {
				// The history gives each vote the previous epoch as its source,
				// as on a chain that justifies every epoch. A new chain first
				// justifies epoch 2, as epoch 3 starts.
				gomega.Eventually(func(g gomega.Gomega) {
					checkpoints, err := node.Beacon.FinalityCheckpoints(ctx)
					g.Expect(err).NotTo(gomega.HaveOccurred())
					g.Expect(checkpoints.CurrentJustified.Epoch).To(gomega.BeNumerically(">", 0))
				}).WithContext(ctx).
					WithTimeout(4*val.Chain.EpochDuration).
					WithPolling(rewards.PollInterval).
					Should(gomega.Succeed(), "the chain has justified no epoch")

				// The history covers the epoch the key arrives in, so it covers
				// every vote the client could make before last+1.
				head := testsuite.MustSucceed(node.Beacon.HeadSlot(ctx))
				first = max(val.Chain.EpochAt(time.Now()), val.Chain.Epoch(head))
				last = first + aheadEpochs
				imported = testsuite.MustSucceed(interchange.ConsecutiveAttestations(
					genesisValidatorsRoot, publicKey, first, last))
				history := testsuite.MustSucceed(imported.Encode())
				ginkgo.GinkgoWriter.Printf("importing a history of attestations for targets %d to %d: %s\n", first, last, history)

				gomega.Expect(validatorClient.Keymanager.ImportKeystoreWithHistory(
					ctx, keystore, validatorops.KeystorePassword, history)).To(gomega.Succeed())
				keystores := testsuite.MustSucceed(validatorClient.Keymanager.ListKeystores(ctx))
				gomega.Expect(keymanager.ContainsPublicKey(keystores, publicKey)).To(gomega.BeTrue(),
					"imported key is not in the validator client")
			}, ginkgo.SpecTimeout(importTimeout))

		ginkgo.It("casts no vote for any target its imported history covers", func(ctx ginkgo.SpecContext) {
			val.AwaitRewards(ctx, last)
			for epoch := first; epoch <= last; epoch++ {
				reward := testsuite.MustSucceed(val.AttestationReward(ctx, epoch))
				amounts := testsuite.MustSucceed(val.VoteAmounts(ctx, epoch))
				gomega.Expect(reward).To(gomega.Equal(beacon.AttestationReward{
					ValidatorIndex: val.Index,
					Source:         -int64(amounts.MissedSource),
					Target:         -int64(amounts.MissedTarget),
				}), "attestation rewards of epoch %d, which the imported history covers", epoch)
			}
		}, ginkgo.SpecTimeout(refusalTimeout))

		ginkgo.It("votes again from the first target after its imported history", func(ctx ginkgo.SpecContext) {
			next := last + 1
			val.AwaitRewards(ctx, next)
			reward := testsuite.MustSucceed(val.AttestationReward(ctx, next))
			gomega.Expect(reward.Target).To(gomega.BeNumerically(">", 0),
				"the target vote of epoch %d did not count: %+v", next, reward)
		}, ginkgo.SpecTimeout(resumeTimeout))

		ginkgo.It("exports the imported history, with only its later votes added, when its key is deleted",
			func(ctx ginkgo.SpecContext) {
				raw := testsuite.MustSucceed(validatorClient.Keymanager.DeleteKeystore(ctx, publicKey))
				ginkgo.GinkgoWriter.Printf("exported history: %s\n", raw)
				exported := testsuite.MustSucceed(interchange.Parse(raw))
				gomega.Expect(interchange.SameHex(exported.Metadata.GenesisValidatorsRoot, genesisValidatorsRoot)).
					To(gomega.BeTrue(), "exported genesis validators root %s, want %s",
						exported.Metadata.GenesisValidatorsRoot, genesisValidatorsRoot)

				// The client records a vote before it publishes one, so the
				// covered targets hold only the imported votes.
				var covered, later []interchange.Attestation
				for _, attestation := range exported.Attestations(publicKey) {
					if attestation.TargetEpoch <= last {
						covered = append(covered, attestation)
					} else {
						later = append(later, attestation)
					}
				}
				gomega.Expect(covered).To(gomega.ConsistOf(imported.Attestations(publicKey)),
					"exported votes for targets up to %d", last)
				gomega.Expect(later).To(gomega.ContainElement(gomega.HaveField("TargetEpoch", last+1)),
					"exported votes for targets after %d", last)
			}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("leaves every validator unslashed, and the chain keeps finalizing", func(ctx ginkgo.SpecContext) {
			gomega.Expect(testsuite.MustSucceed(val.Client.SlashedValidators(ctx))).To(gomega.BeEmpty(), "slashed validators")
			testsuite.ExpectFinalizing(ctx, val.Client, val.Chain)
		}, ginkgo.SpecTimeout(time.Minute))
	},
)
