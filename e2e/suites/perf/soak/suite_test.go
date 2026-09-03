//go:build e2e

package soak

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/manifest"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/internal/jsonfile"
	"github.com/cyyber/qrl-tests/internal/soak"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/qrlclient"
)

const (
	samplesFile  = "samples.jsonl"
	resultsFile  = "results.json"
	verdictFile  = "verdict.json"
	defaultSlots = 8
)

type liveSuite struct {
	environment  devnet.Environment
	reportDir    string
	duration     time.Duration
	enforce      bool
	loadPercent  int
	thresholds   soak.Thresholds
	participants []soak.Endpoints
	kube         *soak.Kube
	client       *qrlclient.Client
	from         common.Address
	signer       bind.SignerFn
	chainID      *big.Int
	placement    []soak.Placement
}

func setupLiveSuite(ctx context.Context) *liveSuite {
	ginkgo.GinkgoHelper()

	runtime := testsuite.LoadRuntime()
	suiteManifest := testsuite.MustSucceed(manifest.FromEnv())
	node := testsuite.MustSucceed(runtime.PrimaryNode(ctx))
	transactor := testsuite.MustSucceed(bind.NewKeyedTransactorWithChainID(node.Wallet, node.ChainID))

	thresholds := testsuite.MustSucceed(soak.LoadThresholds(os.Getenv("SOAK_THRESHOLDS")))
	duration := parseDuration("SOAK_DURATION", 4*time.Hour)
	if duration <= 0 {
		ginkgo.Fail("SOAK_DURATION must be positive")
	}

	suite := &liveSuite{
		environment: suiteManifest.Environment,
		reportDir:   filepath.Dir(os.Getenv(manifest.PathEnv)),
		duration:    duration,
		enforce:     parseBool("SOAK_ENFORCE"),
		loadPercent: parseInt("SOAK_LOAD_PERCENT", devnet.DefaultLoadPercent),
		thresholds:  thresholds,
		client:      node.Execution,
		from:        transactor.From,
		signer:      transactor.Signer,
		chainID:     node.ChainID,
	}
	for _, participant := range suite.environment.Participants {
		suite.participants = append(suite.participants, soak.Endpoints{
			Index:        participant.Index,
			ExecutionRPC: participant.Execution.RPCURL,
			ConsensusAPI: participant.Consensus.URL,
			Metrics: map[soak.Client]string{
				soak.ClientExecution: participant.Execution.MetricsURL,
				soak.ClientConsensus: participant.Consensus.MetricsURL,
				soak.ClientValidator: participant.Validator.MetricsURL,
			},
		})
	}
	if suite.environment.Backend == devnet.BackendKubernetes {
		kube, err := soak.InClusterKube(suite.environment.Namespace())
		if err != nil {
			ginkgo.GinkgoWriter.Printf("in-cluster kube unavailable: %v\n", err)
		} else {
			suite.kube = kube
		}
	}
	return suite
}

func (suite *liveSuite) assertPlacement(ctx context.Context) {
	ginkgo.GinkgoHelper()
	if suite.kube == nil {
		ginkgo.Skip("placement is verified only inside the Kubernetes cluster")
	}
	placements, err := soak.VerifyPlacement(ctx, suite.kube, devnet.ParticipantNodeLabel, len(suite.participants))
	suite.placement = placements
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "one participant per labelled work node")
}

func (suite *liveSuite) assertSoak(ctx context.Context) {
	ginkgo.GinkgoHelper()

	samplesPath := filepath.Join(suite.reportDir, samplesFile)
	samplesFile := testsuite.MustSucceed(os.OpenFile(samplesPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600))
	ginkgo.DeferCleanup(samplesFile.Close)

	sampler := &soak.Sampler{
		Participants:  suite.participants,
		Thresholds:    suite.thresholds,
		Interval:      parseDuration("SOAK_INTERVAL", 30*time.Second),
		Kube:          suite.kube,
		Canary:        suite,
		SlotsPerEpoch: defaultSlots,
		Out:           samplesFile,
		Log:           log.New(ginkgo.GinkgoWriter, "soak: ", 0),
	}

	samples, err := sampler.Run(ctx, suite.duration)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "sample the soak window")
	gomega.Expect(samples).NotTo(gomega.BeEmpty(), "the sampler produced no samples")

	evaluation := soak.Evaluate(samples, suite.thresholds, soak.Options{
		Participants:  len(suite.participants),
		SlotsPerEpoch: defaultSlots,
		Enforce:       suite.enforce,
		LoadPercent:   suite.loadPercent,
		Placement:     suite.placement,
	})
	testsuite.MustSucceed(0, jsonfile.Write(filepath.Join(suite.reportDir, resultsFile), evaluation, "soak results"))
	testsuite.MustSucceed(0, jsonfile.Write(filepath.Join(suite.reportDir, verdictFile), map[string]any{
		"passed":   evaluation.Passed,
		"enforced": evaluation.Enforced,
		"class":    verdictClass(evaluation),
	}, "soak verdict"))

	if !evaluation.Passed {
		detail, _ := json.MarshalIndent(failedGates(evaluation), "", "  ")
		ginkgo.Fail(fmt.Sprintf("soak gates failed:\n%s", detail))
	}
	if !suite.enforce {
		ginkgo.GinkgoWriter.Println("SOAK_ENFORCE is off; gate breaches are recorded but do not fail the lane")
	}
}

func (suite *liveSuite) Send(ctx context.Context, timeout time.Duration) soak.Canary {
	canary := soak.Canary{SentAt: time.Now().UTC()}
	wait, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	auth := &bind.TransactOpts{From: suite.from, Signer: suite.signer, Context: wait, Value: big.NewInt(1)}
	tx, err := sendValue(wait, suite.client, auth, suite.from)
	if err != nil {
		canary.Error = err.Error()
		return canary
	}
	receipt, err := bind.WaitMined(wait, suite.client, tx)
	if err != nil {
		canary.Error = err.Error()
		return canary
	}
	if receipt == nil || receipt.Status != types.ReceiptStatusSuccessful {
		canary.Error = "transaction was mined unsuccessful"
		return canary
	}
	canary.Included = true
	canary.Latency = time.Since(canary.SentAt)
	if receipt.BlockNumber != nil {
		canary.Block = receipt.BlockNumber.Uint64()
	}
	return canary
}

func sendValue(ctx context.Context, client *qrlclient.Client, auth *bind.TransactOpts, to common.Address) (*types.Transaction, error) {
	nonce, err := client.PendingNonceAt(ctx, auth.From)
	if err != nil {
		return nil, err
	}
	tip, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, err
	}
	header, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, err
	}
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, err
	}
	base := big.NewInt(0)
	if header.BaseFee != nil {
		base = header.BaseFee
	}
	feeCap := new(big.Int).Add(new(big.Int).Mul(base, big.NewInt(2)), tip)
	unsigned := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: tip,
		GasFeeCap: feeCap,
		Gas:       21_000,
		To:        &to,
		Value:     auth.Value,
	})
	signed, err := auth.Signer(auth.From, unsigned)
	if err != nil {
		return nil, err
	}
	if err := client.SendTransaction(ctx, signed); err != nil {
		return nil, err
	}
	return signed, nil
}

func failedGates(evaluation soak.Evaluation) []soak.Gate {
	var failed []soak.Gate
	for _, gate := range evaluation.Gates {
		if !gate.Passed {
			failed = append(failed, gate)
		}
	}
	return failed
}

func verdictClass(evaluation soak.Evaluation) string {
	if evaluation.Passed {
		return "passed"
	}
	for _, gate := range evaluation.Gates {
		if !gate.Passed && strings.HasPrefix(gate.Name, "placement/") {
			return "infrastructure"
		}
	}
	return "product"
}

func parseDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		ginkgo.Fail(fmt.Sprintf("parse %s=%q: %v", name, value, err))
	}
	return parsed
}

func parseBool(name string) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return false
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		ginkgo.Fail(fmt.Sprintf("parse %s=%q: %v", name, value, err))
	}
	return parsed
}

func parseInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		ginkgo.Fail(fmt.Sprintf("parse %s=%q: %v", name, value, err))
	}
	return parsed
}
