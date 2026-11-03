//go:build e2e

package remotesigner

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"maps"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/keymanager"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/remotesigner"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/internal/containerfiles"
	"github.com/cyyber/qrl-tests/internal/dockerapi"
	dockerclient "github.com/moby/moby/client"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

const (
	// finalizedEpochBeforeProbes leaves finalized epochs whose attestations
	// and blocks the signer recorded, for the refused requests.
	finalizedEpochBeforeProbes = 4
	finalityTimeoutEpochs      = 12
	epochsFinalizedAfterProbes = 2

	// diagnosticsDirectory, in the lane's report directory, receives what
	// the signer reports when a spec fails.
	diagnosticsDirectory = "remote-signer"

	// removedKeyIndex is the validator whose key the remote keys spec
	// removes and adds back, and exitingIndexFromLast, counted back from the
	// last validator, the one the exit spec exits: two validators, so that
	// neither spec depends on the other.
	removedKeyIndex      = 1
	exitingIndexFromLast = 1

	// importProbePath, under the signer's URL, is the url the remote keys
	// spec imports its key with: qrysm ignores that url, so the signer must
	// get no request whose path starts with it.
	importProbePath = "/remote-keys-import-probe"

	// signerURLFlag is the validator client's flag naming its signer.
	signerURLFlag = "--validators-external-signer-url="

	// exitsDisabled is what qrysm's exit command fails with when the signer
	// refuses to sign exits, with 403 ARTIFACT_TYPE_DISABLED.
	exitsDisabled = "remote signer does not sign voluntary exits (start it with --voluntary-exit-signing-enabled to sign them)"
	// exitSubmitted is what the command logs once the beacon node has
	// accepted an exit.
	exitSubmitted = "Voluntary exit was successful"
)

// The duties the validator client signs on this network: every type the
// signer enables but the validator registration, which it signs only for a
// builder.
var dutyTypes = []string{
	remotesigner.TypeBlockV2,
	remotesigner.TypeAttestation,
	remotesigner.TypeAggregationSlot,
	remotesigner.TypeAggregateAndProof,
	remotesigner.TypeSyncCommitteeMessage,
	remotesigner.TypeSyncCommitteeSelectionProof,
	remotesigner.TypeSyncCommitteeContributionAndProof,
}

// The types the signer enables by default: exits are off unless it runs with
// --voluntary-exit-signing-enabled.
var enabledTypes = append(slices.Clone(dutyTypes), remotesigner.TypeValidatorRegistration)

// farFutureEpoch is the exit epoch of a validator that is not exiting.
const farFutureEpoch = ^uint64(0)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Remote signer E2E suite")
}

var _ = ginkgo.Describe(
	"A validator client that signs through go-remotesigner",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.ContinueOnFailure,
	ginkgo.Label("e2e", "consensus", "remote-signer"),
	func() {
		var (
			node      *live.Node
			chain     chaininfo.Info
			genesis   beacon.Genesis
			docker    dockerclient.APIClient
			signer    *remotesigner.Client
			service   devnet.RemoteSignerService
			validator devnet.ValidatorService
			// keys is the validator client's key file: the keys it asks the
			// signer to sign with.
			keys     []string
			forkInfo remotesigner.ForkInfo
			// mtls is set when the lane's profile runs the signer in
			// token+mtls mode, and withoutCertificate is then signer without
			// the validator client's client certificate; in token mode the
			// two clients are the same.
			mtls               bool
			withoutCertificate *remotesigner.Client
			// signsExits is set when the lane's profile runs the signer with
			// --voluntary-exit-signing-enabled.
			signsExits bool
		)

		finalizedEpoch := func(ctx context.Context) (uint64, error) {
			checkpoints, err := node.Beacon.FinalityCheckpoints(ctx)
			return checkpoints.Finalized.Epoch, err
		}

		scrape := func(ctx context.Context) remotesigner.Metrics {
			ginkgo.GinkgoHelper()
			return testsuite.MustSucceed(remotesigner.Scrape(ctx, service.MetricsURL))
		}

		sum := func(metrics remotesigner.Metrics, name string, match remotesigner.Matcher) float64 {
			ginkgo.GinkgoHelper()
			return testsuite.MustSucceed(metrics.Sum(name, match))
		}

		expectRefused := func(err error, reason string) {
			ginkgo.GinkgoHelper()
			refusal, ok := remotesigner.AsResponseError(err)
			gomega.Expect(ok).To(gomega.BeTrue(), "the signer did not refuse the request: %v", err)
			gomega.Expect(refusal.StatusCode).To(gomega.Equal(http.StatusPreconditionFailed), "%v", err)
			gomega.Expect(refusal.Reason).To(gomega.Equal(reason), "%v", err)
		}

		// expectNoFaults checks the counters that must stay at zero whatever
		// the signer was asked.
		expectNoFaults := func(metrics remotesigner.Metrics) {
			ginkgo.GinkgoHelper()
			gomega.Expect(sum(metrics, remotesigner.MetricSigningRequests, remotesigner.OneOf("result", "bad_request", "error"))).
				To(gomega.BeZero(), "sign requests the signer could not serve")
			gomega.Expect(sum(metrics, remotesigner.MetricRandaoReveals, remotesigner.OneOf("result", "refused", "error"))).
				To(gomega.BeZero(), "RANDAO reveals the signer refused or failed")
			gomega.Expect(sum(metrics, remotesigner.MetricRandaoRateLimited, nil)).
				To(gomega.BeZero(), "RANDAO reveals refused by the new-layer bucket")
			gomega.Expect(sum(metrics, remotesigner.MetricVerifyFailures, nil)).
				To(gomega.BeZero(), "signatures that failed verify-before-release")
			gomega.Expect(sum(metrics, remotesigner.MetricHTTPRequestDurations, remotesigner.ServerFaults())).
				To(gomega.BeZero(), "API requests answered with a 5xx other than the 503 of /upcheck while loading")
		}

		expectValidatorClientHealthy := func(ctx context.Context) {
			ginkgo.GinkgoHelper()
			gomega.Expect(validator.MetricsURL).NotTo(gomega.BeEmpty(), "the validator client publishes no metrics port")
			metrics := testsuite.MustSucceed(remotesigner.Scrape(ctx, validator.MetricsURL))
			gomega.Expect(sum(metrics, remotesigner.ValidatorMetricSignRequests, nil)).To(gomega.BeNumerically(">", 0))
			gomega.Expect(sum(metrics, remotesigner.ValidatorMetricRandaoRequests, nil)).To(gomega.BeNumerically(">", 0))
			gomega.Expect(sum(metrics, remotesigner.ValidatorMetricErroredResponses, nil)).
				To(gomega.BeZero(), "the validator client got errors from the signer")
		}

		slotStart := func(slot uint64) time.Time {
			return time.Unix(int64(genesis.Time), 0).Add(time.Duration(slot) * chain.SlotDuration)
		}

		sleepUntil := func(ctx context.Context, at time.Time) {
			ginkgo.GinkgoHelper()
			timer := time.NewTimer(time.Until(at))
			defer timer.Stop()
			select {
			case <-ctx.Done():
				ginkgo.Fail(fmt.Sprintf("waiting until %s: %v", at.Format(time.TimeOnly), context.Cause(ctx)))
			case <-timer.C:
			}
		}

		// Each active validator attests once an epoch, in its slot of the
		// epoch, at a third of the slot or when the slot's block arrives if
		// that is sooner. The signer's ATTESTATION requests from four fifths
		// into the slot before an epoch to four fifths into the epoch's last
		// slot are therefore the epoch's: one for each attester duty of a key
		// the validator client signs for. countFrom is when that window
		// starts.
		countFrom := func(epoch uint64) time.Time {
			return slotStart(epoch*chain.SlotsPerEpoch - 1).Add(chain.SlotDuration * 4 / 5)
		}

		// countableEpoch is the first epoch after the current one whose
		// attestations epochAttestations can still count.
		countableEpoch := func() uint64 {
			epoch := uint64(time.Since(slotStart(0))/chain.EpochDuration) + 1
			for !time.Now().Before(countFrom(epoch)) {
				epoch++
			}
			return epoch
		}

		// epochAttestations returns the ATTESTATION requests the signer got
		// for the duties of epoch, the epoch's attester duties of the
		// validators, and the scrape at the end of the window.
		epochAttestations := func(ctx context.Context, epoch uint64, validators []uint64) (float64, int, remotesigner.Metrics) {
			ginkgo.GinkgoHelper()
			gomega.Expect(time.Now().Before(countFrom(epoch))).To(gomega.BeTrue(), "epoch %d has begun", epoch)
			attestations := remotesigner.With("type", remotesigner.TypeAttestation)
			sleepUntil(ctx, countFrom(epoch))
			before := sum(scrape(ctx), remotesigner.MetricSigningRequests, attestations)
			// The next epoch's duties are known by now.
			duties := testsuite.MustSucceed(node.Beacon.AttesterDuties(ctx, epoch, validators))
			sleepUntil(ctx, countFrom(epoch+1))
			after := scrape(ctx)
			return sum(after, remotesigner.MetricSigningRequests, attestations) - before, len(duties), after
		}

		// targetVote is the validator's target reward in epoch: positive when
		// its vote for the epoch's target was included, else not.
		targetVote := func(ctx context.Context, epoch, index uint64) int64 {
			ginkgo.GinkgoHelper()
			var rewards []beacon.AttestationReward
			// The rewards of an epoch are served two epochs after it.
			gomega.Eventually(func(ctx context.Context) error {
				var err error
				rewards, err = node.Beacon.AttestationRewards(ctx, epoch, []uint64{index})
				return err
			}).WithContext(ctx).WithTimeout(4 * chain.EpochDuration).WithPolling(chain.SlotDuration).Should(gomega.Succeed())
			gomega.Expect(rewards).To(gomega.HaveLen(1))
			return rewards[0].Target
		}

		validatorFlags := func(ctx context.Context) []string {
			ginkgo.GinkgoHelper()
			container := testsuite.MustSucceed(devnet.InspectServiceContainer(ctx, docker, validator.ID))
			return container.Command
		}

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime := testsuite.LoadRuntime()
			authMode := runtime.Profile.RemoteSignerAuthMode()
			gomega.Expect(authMode).NotTo(gomega.BeEmpty(), "the lane's profile %q runs no go-remotesigner", runtime.Profile)
			mtls = authMode == devnet.RemoteSignerAuthTokenMTLS
			signsExits = runtime.Profile.RemoteSignerSignsExits()
			node = testsuite.MustSucceed(runtime.PrimaryNode(ctx))
			service = node.Participant.RemoteSigner
			gomega.Expect(service.URL).NotTo(gomega.BeEmpty(),
				"the network runs no go-remotesigner: start it with the %s profile", runtime.Profile)
			validator = node.Participant.Validator
			gomega.Expect(validator.ID).NotTo(gomega.BeEmpty(), "the participant runs no validator client")
			chain = testsuite.MustSucceed(chaininfo.Load(ctx, node.Beacon))
			genesis = testsuite.MustSucceed(node.Beacon.Genesis(ctx))
			forkInfo = remotesigner.GenesisForkInfo(genesis.ForkVersion, genesis.ValidatorsRoot)

			docker = testsuite.MustSucceed(dockerapi.New())
			ginkgo.DeferCleanup(docker.Close)
			// The validator client's files: the token and the client key are
			// never logged.
			readFile := func(file string) []byte {
				ginkgo.GinkgoHelper()
				return testsuite.MustSucceed(devnet.ReadServiceFile(ctx, docker, validator.ID, file))
			}
			keys = strings.Fields(string(readFile(devnet.ValidatorRemoteSignerKeyFile)))
			token := strings.TrimSpace(string(readFile(devnet.ValidatorRemoteSignerTokenFile)))
			withoutCertificate = testsuite.MustSucceed(remotesigner.New(service.URL, readFile(devnet.ValidatorRemoteSignerCAFile), token))
			signer = withoutCertificate
			if mtls {
				signer = testsuite.MustSucceed(withoutCertificate.WithClientCertificate(
					readFile(devnet.ValidatorRemoteSignerClientCertFile), readFile(devnet.ValidatorRemoteSignerClientKeyFile)))
			}
		}, ginkgo.NodeTimeout(time.Minute))

		ginkgo.AfterEach(func(ctx ginkgo.SpecContext) {
			report := ginkgo.CurrentSpecReport()
			if !report.Failed() {
				return
			}
			if signer == nil {
				ginkgo.GinkgoWriter.Println("no remote signer diagnostics: the suite never reached the signer")
				return
			}
			dir := filepath.Join(testsuite.LaneReportDir(), diagnosticsDirectory, slug(report.LeafNodeText))
			if err := remotesigner.WriteDiagnostics(ctx, dir, signer, service.MetricsURL, validator.MetricsURL); err != nil {
				ginkgo.GinkgoWriter.Printf("remote signer diagnostics are incomplete: %v\n", err)
			}
			ginkgo.GinkgoWriter.Printf("remote signer diagnostics: %s\n", dir)
		}, ginkgo.NodeTimeout(time.Minute))

		ginkgo.It("serves exactly the validator client's keys", func(ctx ginkgo.SpecContext) {
			gomega.Expect(signer.Upcheck(ctx)).To(gomega.Succeed())
			gomega.Expect(keys).NotTo(gomega.BeEmpty(), "the validator client's key file lists no key")

			served := testsuite.MustSucceed(signer.PublicKeys(ctx))
			gomega.Expect(slices.IsSorted(served)).To(gomega.BeTrue(), "the signer serves its keys unsorted")
			gomega.Expect(sortedLower(served)).To(gomega.Equal(sortedLower(keys)),
				"the signer serves %d keys, the validator client's key file lists %d", len(served), len(keys))

			// The single participant's validator client holds every validator
			// of the network, which are the genesis validators: none of them
			// signs elsewhere, so finality depends on the signer alone.
			validators := testsuite.MustSucceed(node.Beacon.Validators(ctx))
			networkKeys := make([]string, len(validators))
			for index, record := range validators {
				networkKeys[index] = record.PublicKey
			}
			gomega.Expect(sortedLower(networkKeys)).To(gomega.Equal(sortedLower(keys)),
				"the network has %d validators, the validator client's key file lists %d", len(networkKeys), len(keys))

			for name, client := range map[string]*remotesigner.Client{
				"no token":      signer.WithToken(""),
				"a wrong token": signer.WithToken("0x" + strings.Repeat("0", 64)),
			} {
				_, err := client.PublicKeys(ctx)
				refusal, ok := remotesigner.AsResponseError(err)
				gomega.Expect(ok).To(gomega.BeTrue(), "with %s: %v", name, err)
				gomega.Expect(refusal.StatusCode).To(gomega.Equal(http.StatusUnauthorized), "with %s", name)
				gomega.Expect(refusal.Authenticate).To(gomega.HavePrefix("Bearer"), "with %s", name)
			}

			if mtls {
				// Without a client certificate the handshake fails on every
				// route, /upcheck included: there is no HTTP answer to it.
				for name, call := range map[string]func(context.Context) error{
					"GET /upcheck": withoutCertificate.Upcheck,
					"GET publicKeys": func(ctx context.Context) error {
						_, err := withoutCertificate.PublicKeys(ctx)
						return err
					},
				} {
					err := call(ctx)
					gomega.Expect(err).To(gomega.HaveOccurred(), "the signer served %s without a client certificate", name)
					_, answered := remotesigner.AsResponseError(err)
					gomega.Expect(answered).To(gomega.BeFalse(),
						"the signer answered %s without a client certificate: %v", name, err)
				}
			}
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("is bound to the chain the validator client follows", func(ctx ginkgo.SpecContext) {
			capabilities := testsuite.MustSucceed(signer.Capabilities(ctx))
			gomega.Expect(capabilities.APIVersion).To(gomega.HavePrefix("1."))
			gomega.Expect(capabilities.RandaoReveal).To(gomega.BeTrue(), "the signer cannot release RANDAO reveals")
			gomega.Expect(capabilities.SlashingProtection).To(gomega.Equal("bolt"))
			types := enabledTypes
			if signsExits {
				types = append(slices.Clone(enabledTypes), remotesigner.TypeVoluntaryExit)
			}
			gomega.Expect(capabilities.Types).To(gomega.ConsistOf(types))

			network := capabilities.Network
			gomega.Expect(strings.ToLower(network.GenesisForkVersion)).To(gomega.Equal(strings.ToLower(genesis.ForkVersion)))
			gomega.Expect(network.GenesisValidatorsRoot).NotTo(gomega.BeNil(), "the signer has no genesis validators root")
			gomega.Expect(strings.ToLower(*network.GenesisValidatorsRoot)).To(gomega.Equal(strings.ToLower(genesis.ValidatorsRoot)))
			gomega.Expect(network.GenesisTime).To(gomega.HaveValue(gomega.Equal(strconv.FormatUint(genesis.Time, 10))))
			secondsPerSlot := testsuite.MustSucceed(node.Beacon.SpecUint(ctx, "SECONDS_PER_SLOT"))
			gomega.Expect(network.SecondsPerSlot).To(gomega.Equal(strconv.FormatUint(secondsPerSlot, 10)))
			gomega.Expect(network.SlotsPerEpoch).To(gomega.Equal(strconv.FormatUint(chain.SlotsPerEpoch, 10)))

			health := testsuite.MustSucceed(signer.Healthcheck(ctx))
			gomega.Expect(health.Status).To(gomega.Equal("UP"))
			gomega.Expect(health.Checks).To(gomega.ContainElement(
				gomega.HaveField("ID", "keys-check/keystores-bulk-loading")))
			for _, check := range health.Checks {
				gomega.Expect(check.Status).To(gomega.Equal("UP"), "healthcheck %s", check.ID)
			}
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("gives the validator client no keystore", func(ctx ginkgo.SpecContext) {
			container := testsuite.MustSucceed(devnet.InspectServiceContainer(ctx, docker, validator.ID))
			arguments := container.Command[1:]
			gomega.Expect(arguments).To(gomega.ContainElement(
				gomega.HavePrefix("--validators-external-signer-url=https://" + service.Name + ":")))
			flags := []string{
				"--validators-external-signer-key-file=" + devnet.ValidatorRemoteSignerKeyFile,
				"--validators-external-signer-auth-token-file=" + devnet.ValidatorRemoteSignerTokenFile,
				"--validators-external-signer-tls-ca-file=" + devnet.ValidatorRemoteSignerCAFile,
			}
			wantFiles := []string{
				devnet.ValidatorRemoteSignerTokenFile, devnet.ValidatorRemoteSignerKeyFile, devnet.ValidatorRemoteSignerCAFile,
			}
			if mtls {
				flags = append(flags,
					"--validators-external-signer-tls-client-cert-file="+devnet.ValidatorRemoteSignerClientCertFile,
					"--validators-external-signer-tls-client-key-file="+devnet.ValidatorRemoteSignerClientKeyFile)
				wantFiles = append(wantFiles,
					devnet.ValidatorRemoteSignerClientCertFile, devnet.ValidatorRemoteSignerClientKeyFile)
			} else {
				gomega.Expect(arguments).NotTo(gomega.ContainElement(
					gomega.HavePrefix("--validators-external-signer-tls-client-")))
			}
			for _, flag := range flags {
				gomega.Expect(arguments).To(gomega.ContainElement(flag))
			}
			gomega.Expect(arguments).To(gomega.ContainElement(gomega.HavePrefix("--datadir=")))
			for _, flag := range []string{"--wallet-dir", "--wallet-password-file", "--interop-num-validators"} {
				gomega.Expect(arguments).NotTo(gomega.ContainElement(gomega.HavePrefix(flag)))
			}

			filesDir := path.Dir(devnet.ValidatorRemoteSignerKeyFile)
			gomega.Expect(container.Mounts).To(gomega.ContainElement(filesDir))
			for _, mount := range []string{"/validator-keys", "/qrysm-password"} {
				gomega.Expect(container.Mounts).NotTo(gomega.ContainElement(mount))
			}
			files := testsuite.MustSucceed(containerfiles.Read(ctx, docker, container.ID, filesDir))
			names := make([]string, len(files))
			for index, file := range files {
				names[index] = file.Name
			}
			gomega.Expect(names).To(gomega.ConsistOf(wantFiles))

			_, err := devnet.ReadServiceFile(ctx, docker, validator.ID, "/validator-keys")
			gomega.Expect(err).To(gomega.Satisfy(cerrdefs.IsNotFound), "the validator client has /validator-keys: %v", err)
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("finalizes on the signer's signatures", func(ctx ginkgo.SpecContext) {
			gomega.Eventually(finalizedEpoch).WithContext(ctx).
				WithTimeout(finalityTimeoutEpochs * chain.EpochDuration).
				WithPolling(chain.SlotDuration).
				Should(gomega.BeNumerically(">=", finalizedEpochBeforeProbes))
			gomega.Expect(testsuite.MustSucceed(node.Beacon.SlashedValidators(ctx))).To(gomega.BeEmpty())
			expectValidatorClientHealthy(ctx)
		}, ginkgo.SpecTimeout(10*time.Minute))

		ginkgo.It("signed the duties and refused none of them", func(ctx ginkgo.SpecContext) {
			metrics := scrape(ctx)
			ginkgo.AddReportEntry("signed requests by type",
				metrics.ByLabel(remotesigner.MetricSigningRequests, "type", remotesigner.With("result", "ok")))
			ginkgo.AddReportEntry("RANDAO reveals by result", metrics.ByLabel(remotesigner.MetricRandaoReveals, "result", nil))

			// Every duty went through the signer: finality alone does not
			// show the aggregates and the sync committee's messages.
			for _, artifact := range dutyTypes {
				gomega.Expect(sum(metrics, remotesigner.MetricSigningRequests, remotesigner.With("type", artifact, "result", "ok"))).
					To(gomega.BeNumerically(">", 0), "signed %s requests", artifact)
			}
			gomega.Expect(sum(metrics, remotesigner.MetricRandaoReveals, remotesigner.OneOf("result", "first_use", "new_layer"))).
				To(gomega.BeNumerically(">", 0), "RANDAO reveals that released a new layer")
			// The validator client asks for the slot's RANDAO reveal before it
			// asks for the block, and proposes nothing without it: every
			// block it had signed came after a reveal the signer released.
			blocks := sum(metrics, remotesigner.MetricSigningRequests, remotesigner.With("type", remotesigner.TypeBlockV2, "result", "ok"))
			gomega.Expect(sum(metrics, remotesigner.MetricRandaoReveals,
				remotesigner.OneOf("result", "first_use", "new_layer", "gap_advanced", "re_reveal"))).
				To(gomega.BeNumerically(">=", blocks), "RANDAO reveals released for the %v blocks signed", blocks)
			gomega.Expect(sum(metrics, remotesigner.MetricSigningRequests, remotesigner.With("result", "refused"))).
				To(gomega.BeZero(), "sign requests the signer refused")
			gomega.Expect(sum(metrics, remotesigner.MetricPreventedSignings, nil)).
				To(gomega.BeZero(), "signings slashing protection prevented")
			expectNoFaults(metrics)
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("refuses a second attestation for a target epoch it signed", func(ctx ginkgo.SpecContext) {
			finalized := testsuite.MustSucceed(finalizedEpoch(ctx))
			gomega.Expect(finalized).To(gomega.BeNumerically(">=", 2), "no epoch before the finalized one has a source epoch")
			epoch := finalized - 1

			// A validator whose vote for the epoch's target is on chain: its
			// attestation for that target went through the signer, which
			// recorded it.
			indices := make([]uint64, len(keys))
			for index := range indices {
				indices[index] = uint64(index)
			}
			rewards := testsuite.MustSucceed(node.Beacon.AttestationRewards(ctx, epoch, indices))
			voter := slices.IndexFunc(rewards, func(reward beacon.AttestationReward) bool { return reward.Target > 0 })
			gomega.Expect(voter).NotTo(gomega.Equal(-1), "no validator voted for the target of epoch %d", epoch)
			record := testsuite.MustSucceed(node.Beacon.Validator(ctx, strconv.FormatUint(rewards[voter].ValidatorIndex, 10)))

			before := scrape(ctx)
			_, err := signer.Sign(ctx, record.PublicKey, remotesigner.AttestationRequest(forkInfo, remotesigner.AttestationData{
				Slot:            epoch * chain.SlotsPerEpoch,
				BeaconBlockRoot: filledRoot(0x0b),
				Source:          remotesigner.Checkpoint{Epoch: epoch - 1, Root: filledRoot(0x5c)},
				Target:          remotesigner.Checkpoint{Epoch: epoch, Root: filledRoot(0x7a)},
			}))
			expectRefused(err, remotesigner.ReasonDoubleVote)

			after := scrape(ctx)
			prevented := remotesigner.With("reason", strings.ToLower(remotesigner.ReasonDoubleVote))
			gomega.Expect(sum(after, remotesigner.MetricPreventedSignings, prevented) -
				sum(before, remotesigner.MetricPreventedSignings, prevented)).To(gomega.Equal(1.0))
			refused := remotesigner.With("type", remotesigner.TypeAttestation, "result", "refused")
			gomega.Expect(sum(after, remotesigner.MetricSigningRequests, refused) -
				sum(before, remotesigner.MetricSigningRequests, refused)).To(gomega.Equal(1.0))
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("refuses a second block for a slot it signed", func(ctx ginkgo.SpecContext) {
			// The finalized checkpoint's block, or the last block before it.
			block := testsuite.MustSucceed(node.Beacon.BlockHeader(ctx, "finalized"))
			gomega.Expect(block.Slot).To(gomega.BeNumerically(">", 0), "no block is finalized yet")
			proposer := testsuite.MustSucceed(node.Beacon.Validator(ctx, strconv.FormatUint(block.ProposerIndex, 10)))

			before := scrape(ctx)
			_, err := signer.Sign(ctx, proposer.PublicKey, remotesigner.BlockRequest(forkInfo, remotesigner.BeaconBlockHeader{
				Slot:          block.Slot,
				ProposerIndex: block.ProposerIndex,
				ParentRoot:    block.ParentRoot,
				StateRoot:     block.StateRoot,
				BodyRoot:      otherRoot(block.BodyRoot),
			}))
			expectRefused(err, remotesigner.ReasonDoubleProposal)

			after := scrape(ctx)
			prevented := remotesigner.With("reason", strings.ToLower(remotesigner.ReasonDoubleProposal))
			gomega.Expect(sum(after, remotesigner.MetricPreventedSignings, prevented) -
				sum(before, remotesigner.MetricPreventedSignings, prevented)).To(gomega.Equal(1.0))
			refused := remotesigner.With("type", remotesigner.TypeBlockV2, "result", "refused")
			gomega.Expect(sum(after, remotesigner.MetricSigningRequests, refused) -
				sum(before, remotesigner.MetricSigningRequests, refused)).To(gomega.Equal(1.0))
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("keeps finalizing after the refusals", func(ctx ginkgo.SpecContext) {
			start := testsuite.MustSucceed(finalizedEpoch(ctx))
			gomega.Eventually(finalizedEpoch).WithContext(ctx).
				WithTimeout((epochsFinalizedAfterProbes + 4) * chain.EpochDuration).
				WithPolling(chain.SlotDuration).
				Should(gomega.BeNumerically(">=", start+epochsFinalizedAfterProbes))

			// The two requests above are the only ones it refused: a refusal
			// records nothing, so no later duty of the validator client is
			// refused.
			metrics := scrape(ctx)
			gomega.Expect(nonZero(metrics.ByLabel(remotesigner.MetricPreventedSignings, "reason", nil))).
				To(gomega.Equal(map[string]float64{
					strings.ToLower(remotesigner.ReasonDoubleVote):     1,
					strings.ToLower(remotesigner.ReasonDoubleProposal): 1,
				}), "signings slashing protection prevented, by reason")
			gomega.Expect(nonZero(metrics.ByLabel(remotesigner.MetricSigningRequests, "type", remotesigner.With("result", "refused")))).
				To(gomega.Equal(map[string]float64{remotesigner.TypeAttestation: 1, remotesigner.TypeBlockV2: 1}),
					"sign requests the signer refused, by type")
			expectNoFaults(metrics)
			gomega.Expect(testsuite.MustSucceed(node.Beacon.SlashedValidators(ctx))).To(gomega.BeEmpty())
			expectValidatorClientHealthy(ctx)
		}, ginkgo.SpecTimeout(5*time.Minute))

		ginkgo.It("stops signing for a key the remote keys API removes, and signs for it again once it is added back", func(ctx ginkgo.SpecContext) {
			gomega.Expect(validator.URL).NotTo(gomega.BeEmpty(), "the validator client serves no keymanager API")
			token := testsuite.MustSucceed(devnet.ReadServiceFile(ctx, docker, validator.ID, devnet.ValidatorKeymanagerTokenFile))
			api := testsuite.MustSucceed(keymanager.New(validator.URL, lastLine(token)))
			var signerURL string
			for _, flag := range validatorFlags(ctx) {
				if value, ok := strings.CutPrefix(flag, signerURLFlag); ok {
					signerURL = value
				}
			}
			gomega.Expect(signerURL).NotTo(gomega.BeEmpty(), "the validator client has no %s", signerURLFlag)
			keyFile := func(ctx context.Context) []string {
				ginkgo.GinkgoHelper()
				return sortedLower(strings.Fields(string(testsuite.MustSucceed(
					devnet.ReadServiceFile(ctx, docker, validator.ID, devnet.ValidatorRemoteSignerKeyFile)))))
			}
			listed := func(ctx context.Context) []string {
				ginkgo.GinkgoHelper()
				remoteKeys := testsuite.MustSucceed(api.ListRemoteKeys(ctx))
				listed := make([]string, len(remoteKeys))
				for index, key := range remoteKeys {
					gomega.Expect(key.URL).To(gomega.Equal(signerURL), "the signer of %s", key.PublicKey)
					gomega.Expect(key.ReadOnly).To(gomega.BeFalse(), "%s, a key of the key file, is read-only", key.PublicKey)
					listed[index] = key.PublicKey
				}
				return sortedLower(listed)
			}

			// The API lists exactly the signer's keys.
			signerKeys := sortedLower(testsuite.MustSucceed(signer.PublicKeys(ctx)))
			gomega.Expect(listed(ctx)).To(gomega.Equal(signerKeys), "the remote keys, and the signer's %d keys", len(signerKeys))
			gomega.Expect(keyFile(ctx)).To(gomega.Equal(signerKeys), "the key file, and the signer's %d keys", len(signerKeys))
			record := testsuite.MustSucceed(node.Beacon.Validator(ctx, strconv.Itoa(removedKeyIndex)))
			gomega.Expect(record.Status).To(gomega.Equal("active_ongoing"), "validator %d", removedKeyIndex)
			removed := strings.ToLower(record.PublicKey)
			others := slices.DeleteFunc(slices.Clone(signerKeys), func(key string) bool { return key == removed })
			gomega.Expect(others).To(gomega.HaveLen(len(signerKeys)-1), "the signer does not hold validator %d's key", removedKeyIndex)
			var indices []uint64
			for _, record := range testsuite.MustSucceed(node.Beacon.Validators(ctx)) {
				if slices.Contains(signerKeys, strings.ToLower(record.PublicKey)) {
					indices = append(indices, record.Index)
				}
			}

			status := testsuite.MustSucceed(api.DeleteRemoteKey(ctx, removed))
			gomega.Expect(strings.EqualFold(status.Status, "deleted")).To(gomega.BeTrue(), "DELETE answered %+v", status)
			// Should a check below fail, the other specs keep the key.
			ginkgo.DeferCleanup(func(ctx ginkgo.SpecContext) {
				if !slices.Contains(keyFile(ctx), removed) {
					_, err := api.ImportRemoteKey(ctx, removed, signerURL)
					gomega.Expect(err).NotTo(gomega.HaveOccurred(), "adding validator %d's key back", removedKeyIndex)
				}
			}, ginkgo.NodeTimeout(time.Minute))
			gomega.Expect(keyFile(ctx)).To(gomega.Equal(others), "the key file after the DELETE")
			gomega.Expect(listed(ctx)).To(gomega.Equal(others), "the remote keys after the DELETE")

			// The validator client refuses the key's duties itself, so the
			// signer gets every attestation of the next whole epoch but the
			// key's, and the key's vote is missing on chain.
			withoutKey := countableEpoch()
			attestations, duties, _ := epochAttestations(ctx, withoutKey, indices)
			gomega.Expect(attestations).To(gomega.Equal(float64(duties-1)),
				"ATTESTATION requests for the %d attester duties of epoch %d, without validator %d's key",
				duties, withoutKey, removedKeyIndex)

			// qrysm ignores the url of an imported key: every key is signed
			// by the signer the validator client names, and nothing asks the
			// url given here, a path of the signer that no route serves.
			unknownPaths := remotesigner.With("route", remotesigner.UnknownPathRoute)
			unknownBefore := sum(scrape(ctx), remotesigner.MetricHTTPRequestDurations, unknownPaths)
			status = testsuite.MustSucceed(api.ImportRemoteKey(ctx, removed, signerURL+importProbePath))
			gomega.Expect(strings.EqualFold(status.Status, "imported")).To(gomega.BeTrue(), "POST answered %+v", status)
			gomega.Expect(keyFile(ctx)).To(gomega.Equal(signerKeys), "the key file after the POST")
			gomega.Expect(listed(ctx)).To(gomega.Equal(signerKeys), "the remote keys after the POST")

			// The validator client takes the key's duties again from the
			// next epoch on.
			withKey := countableEpoch()
			attestations, duties, after := epochAttestations(ctx, withKey, indices)
			gomega.Expect(attestations).To(gomega.Equal(float64(duties)),
				"ATTESTATION requests for the %d attester duties of epoch %d, with validator %d's key again",
				duties, withKey, removedKeyIndex)
			gomega.Expect(sum(after, remotesigner.MetricHTTPRequestDurations, unknownPaths)-unknownBefore).
				To(gomega.BeZero(), "requests to paths the signer does not serve, such as %s", importProbePath)

			gomega.Expect(targetVote(ctx, withoutKey, removedKeyIndex)).To(gomega.BeNumerically("<=", 0),
				"validator %d's target vote in epoch %d, without its key", removedKeyIndex, withoutKey)
			gomega.Expect(targetVote(ctx, withKey, removedKeyIndex)).To(gomega.BeNumerically(">", 0),
				"validator %d's target vote in epoch %d, with its key again", removedKeyIndex, withKey)
			expectNoFaults(scrape(ctx))
		}, ginkgo.SpecTimeout(10*time.Minute))

		// An exit cannot be undone, so it is the suite's last spec.
		ginkgo.It("exits a validator through the signer with qrysm's exit command when the signer signs exits", func(ctx ginkgo.SpecContext) {
			index := uint64(len(keys) - exitingIndexFromLast)
			exiting := testsuite.MustSucceed(node.Beacon.Validator(ctx, strconv.FormatUint(index, 10)))
			gomega.Expect(exiting.Status).To(gomega.Equal("active_ongoing"))

			// qrysm's exit command, next to the validator client with its
			// chain config, beacon node and remote signer flags, as qrysm's
			// remote signer README describes; the chain config is a global
			// flag.
			flags := validatorFlags(ctx)
			command := []string{flags[0]}
			for _, flag := range flags[1:] {
				if strings.HasPrefix(flag, "--chain-config-file=") {
					command = append(command, flag)
				}
			}
			gomega.Expect(command).To(gomega.HaveLen(2), "the validator client has no --chain-config-file")
			command = append(command, "accounts", "voluntary-exit", "--accept-terms-of-use", "--force-exit",
				"--public-keys="+exiting.PublicKey)
			for _, flag := range flags[1:] {
				if strings.HasPrefix(flag, "--validators-external-signer-") || strings.HasPrefix(flag, "--beacon-rpc-provider=") {
					command = append(command, flag)
				}
			}

			exits := func(metrics remotesigner.Metrics, result string) float64 {
				ginkgo.GinkgoHelper()
				return sum(metrics, remotesigner.MetricSigningRequests, remotesigner.With("type", remotesigner.TypeVoluntaryExit, "result", result))
			}
			before := scrape(ctx)
			output, err := devnet.ExecService(ctx, docker, validator.ID, command...)
			after := scrape(ctx)

			if !signsExits {
				// The signer answers 403 ARTIFACT_TYPE_DISABLED, and the command
				// stops, names the signer's option and submits nothing.
				gomega.Expect(err).To(gomega.HaveOccurred(), "the exit command succeeded: %s", output)
				gomega.Expect(err.Error()).To(gomega.ContainSubstring(exitsDisabled))
				gomega.Expect(err.Error()).To(gomega.ContainSubstring("ARTIFACT_TYPE_DISABLED"))
				gomega.Expect(exits(after, "refused") - exits(before, "refused")).To(gomega.Equal(1.0))
				gomega.Expect(exits(after, "ok") - exits(before, "ok")).To(gomega.BeZero())
				gomega.Consistently(func(ctx context.Context) (beacon.Validator, error) {
					return node.Beacon.Validator(ctx, strconv.FormatUint(index, 10))
				}).WithContext(ctx).WithTimeout(2 * chain.SlotDuration).WithPolling(chain.SlotDuration).
					Should(gomega.HaveField("Status", "active_ongoing"))
				return
			}

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(output).To(gomega.ContainSubstring(exitSubmitted))
			gomega.Expect(exits(after, "ok") - exits(before, "ok")).To(gomega.Equal(1.0))
			gomega.Expect(exits(after, "refused") - exits(before, "refused")).To(gomega.BeZero())
			// The beacon node accepted the exit, and a block includes it.
			gomega.Eventually(func(ctx context.Context) (beacon.Validator, error) {
				return node.Beacon.Validator(ctx, strconv.FormatUint(index, 10))
			}).WithContext(ctx).WithTimeout(2 * chain.EpochDuration).WithPolling(chain.SlotDuration).Should(gomega.And(
				gomega.HaveField("Status", "active_exiting"),
				gomega.HaveField("ExitEpoch", gomega.BeNumerically("<", farFutureEpoch)),
			))
		}, ginkgo.SpecTimeout(5*time.Minute))
	},
)

// lastLine is a token file's token: qrysm reads its last non-empty line.
func lastLine(file []byte) string {
	lines := strings.Fields(string(file))
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

func sortedLower(keys []string) []string {
	lowered := make([]string, len(keys))
	for index, key := range keys {
		lowered[index] = strings.ToLower(key)
	}
	slices.Sort(lowered)
	return lowered
}

// nonZero drops the zero values of counters by label.
func nonZero(values map[string]float64) map[string]float64 {
	maps.DeleteFunc(values, func(_ string, value float64) bool { return value == 0 })
	return values
}

// filledRoot is a 32-byte root of one repeated byte.
func filledRoot(fill byte) string {
	return "0x" + hex.EncodeToString(bytes.Repeat([]byte{fill}, 32))
}

// otherRoot is a 32-byte root that differs from root.
func otherRoot(root string) string {
	other := filledRoot(0xd0)
	if strings.EqualFold(other, root) {
		other = filledRoot(0xd1)
	}
	return other
}

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

// slug names a spec's diagnostics directory after its text.
func slug(text string) string {
	return strings.Trim(nonWord.ReplaceAllString(strings.ToLower(text), "-"), "-")
}
