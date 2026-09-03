// Package soak provisions a soak-profile network, samples it, and writes a
// verdict. It is a first-class qrltest command, not an E2E lane.
package soak

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/internal/jsonfile"
	"github.com/cyyber/qrl-tests/perf/internal/soak"
)

const (
	DefaultReportDir    = "reports"
	DefaultDuration     = 4 * time.Hour
	DefaultInterval     = 30 * time.Second
	DefaultStartTimeout = 20 * time.Minute

	SamplesFile  = "samples.jsonl"
	ResultsFile  = "results.json"
	VerdictFile  = "verdict.json"
	SummaryFile  = "summary.md"
	ManifestFile = "manifest.json"
	OutputLog    = "output.log"

	diagnosticsDirectory = "diagnostics"
	cleanupTimeout       = 2 * time.Minute
	diagnosticsTimeout   = 2 * time.Minute
	defaultSlots         = 8
)

type Config struct {
	EnclaveName    string
	ReportDir      string
	Backend        devnet.Backend
	Images         devnet.Images
	Parameters     []byte
	EndpointMode   devnet.EndpointMode
	LoadPercent    int
	StartTimeout   time.Duration
	Duration       time.Duration
	Interval       time.Duration
	Enforce        bool
	KeepNetwork    bool
	Existing       bool
	ThresholdsPath string
}

func (configuration Config) withDefaults() Config {
	configuration.EnclaveName = cmp.Or(configuration.EnclaveName, devnet.DefaultEnclaveName)
	configuration.ReportDir = cmp.Or(configuration.ReportDir, DefaultReportDir)
	configuration.Backend = cmp.Or(configuration.Backend, devnet.BackendDocker)
	configuration.EndpointMode = cmp.Or(configuration.EndpointMode, devnet.EndpointModePublic)
	configuration.StartTimeout = cmp.Or(configuration.StartTimeout, DefaultStartTimeout)
	configuration.Duration = cmp.Or(configuration.Duration, DefaultDuration)
	configuration.Interval = cmp.Or(configuration.Interval, DefaultInterval)
	return configuration
}

type networkManager interface {
	Start(ctx context.Context, options devnet.StartOptions) (devnet.Environment, error)
	Inspect(ctx context.Context, name string) (devnet.Environment, error)
	Stop(ctx context.Context, name string) error
	CollectDiagnostics(ctx context.Context, enclaveName, outputDir string) error
}

type sampleFunc func(ctx context.Context, environment devnet.Environment, reportDir string) (soak.Evaluation, error)

type Runner struct {
	configuration Config
	networks      networkManager
	sample        sampleFunc
	stdout        io.Writer
	stderr        io.Writer
}

func New(configuration Config, stdout, stderr io.Writer) *Runner {
	runner := &Runner{
		configuration: configuration.withDefaults(),
		networks:      devnet.NewManager(),
		stdout:        stdout,
		stderr:        stderr,
	}
	runner.sample = runner.sampleNetwork
	return runner
}

func (runner *Runner) Run(ctx context.Context) (err error) {
	if runner.configuration.Existing && len(runner.configuration.Parameters) != 0 {
		return errors.New("custom parameters cannot be used with an existing network")
	}
	if runner.configuration.Duration <= 0 {
		return errors.New("duration must be positive")
	}

	reportDir, err := filepath.Abs(runner.configuration.ReportDir)
	if err != nil {
		return fmt.Errorf("resolve report directory: %w", err)
	}
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}

	logFile, err := os.OpenFile(filepath.Join(reportDir, OutputLog), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create output log: %w", err)
	}
	defer logFile.Close()
	stdout := io.MultiWriter(runner.stdout, logFile)

	environment, release, err := runner.acquire(ctx)
	if err != nil {
		return err
	}

	defer func() {
		failed := err != nil
		if failed {
			runner.collectDiagnostics(environment.EnclaveName, reportDir)
		}
		if release != nil {
			if cleanupErr := release(); cleanupErr != nil {
				if !failed {
					runner.collectDiagnostics(environment.EnclaveName, reportDir)
				}
				err = errors.Join(err, cleanupErr)
			}
		}
	}()

	if err = jsonfile.Write(filepath.Join(reportDir, ManifestFile), Manifest{
		Profile:     devnet.ProfileSoak,
		Environment: environment,
		Duration:    runner.configuration.Duration,
		LoadPercent: runner.configuration.LoadPercent,
		Enforce:     runner.configuration.Enforce,
	}, "soak manifest"); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "=== soak profile=%s duration=%s enforce=%t ===\n",
		devnet.ProfileSoak, runner.configuration.Duration, runner.configuration.Enforce)

	evaluation, err := runner.sample(ctx, environment, reportDir)
	if err != nil {
		return err
	}
	if err = runner.writeReports(reportDir, evaluation); err != nil {
		return err
	}
	if _, err = io.WriteString(stdout, RenderSummary(evaluation)); err != nil {
		return err
	}
	if !evaluation.Passed {
		return errors.New("soak gates failed")
	}
	if !runner.configuration.Enforce {
		fmt.Fprintln(stdout, "SOAK_ENFORCE is off; gate breaches are recorded but do not fail the run")
	}
	return nil
}

type Manifest struct {
	Profile     devnet.Profile     `json:"profile"`
	Environment devnet.Environment `json:"environment"`
	Duration    time.Duration      `json:"duration"`
	LoadPercent int                `json:"load_percent"`
	Enforce     bool               `json:"enforce"`
}

type Verdict struct {
	Passed   bool   `json:"passed"`
	Enforced bool   `json:"enforced"`
	Class    string `json:"class"`
}

func (runner *Runner) acquire(ctx context.Context) (devnet.Environment, func() error, error) {
	if runner.configuration.Existing {
		environment, err := runner.networks.Inspect(ctx, runner.configuration.EnclaveName)
		if err != nil {
			return devnet.Environment{}, nil, fmt.Errorf("inspect network: %w", err)
		}
		if environment.Backend == "" {
			environment.Backend = runner.configuration.Backend
		}
		return environment, nil, nil
	}

	startCtx, cancel := context.WithTimeout(ctx, runner.configuration.StartTimeout)
	environment, err := runner.networks.Start(startCtx, devnet.StartOptions{
		EnclaveName:           runner.configuration.EnclaveName,
		Backend:               runner.configuration.Backend,
		Images:                runner.configuration.Images,
		Parameters:            runner.configuration.Parameters,
		Profile:               devnet.ProfileSoak,
		EndpointMode:          runner.configuration.EndpointMode,
		LoadPercent:           runner.configuration.LoadPercent,
		FailureDiagnosticsDir: filepath.Join(runner.configuration.ReportDir, diagnosticsDirectory),
	})
	cancel()
	if err != nil {
		return devnet.Environment{}, nil, fmt.Errorf("start network: %w", err)
	}

	var release func() error
	if !runner.configuration.KeepNetwork {
		release = func() error {
			stopCtx, cancelStop := context.WithTimeout(context.Background(), cleanupTimeout)
			defer cancelStop()
			if err := runner.networks.Stop(stopCtx, environment.EnclaveName); err != nil {
				return fmt.Errorf("stop network: %w", err)
			}
			return nil
		}
	}
	return environment, release, nil
}

func (runner *Runner) collectDiagnostics(enclaveName, reportDir string) {
	collectCtx, cancel := context.WithTimeout(context.Background(), diagnosticsTimeout)
	defer cancel()
	if err := runner.networks.CollectDiagnostics(collectCtx, enclaveName, filepath.Join(reportDir, diagnosticsDirectory)); err != nil {
		fmt.Fprintf(runner.stderr, "collect diagnostics: %v\n", err)
	}
}

func (runner *Runner) sampleNetwork(ctx context.Context, environment devnet.Environment, reportDir string) (soak.Evaluation, error) {
	thresholds, err := soak.LoadThresholds(runner.configuration.ThresholdsPath)
	if err != nil {
		return soak.Evaluation{}, err
	}

	var kube *soak.Kube
	if environment.Backend == devnet.BackendKubernetes {
		kube, err = soak.InClusterKube(environment.Namespace())
		if err != nil {
			fmt.Fprintf(runner.stderr, "in-cluster kube unavailable: %v\n", err)
			kube = nil
		}
	}

	var placement []soak.Placement
	if kube != nil {
		placement, err = soak.VerifyPlacement(ctx, kube, devnet.ParticipantNodeLabel, len(environment.Participants))
		if err != nil {
			return soak.Evaluation{}, fmt.Errorf("placement: %w", err)
		}
	}

	samplesPath := filepath.Join(reportDir, SamplesFile)
	samplesFile, err := os.OpenFile(samplesPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return soak.Evaluation{}, fmt.Errorf("create samples file: %w", err)
	}
	defer samplesFile.Close()

	canary, err := newCanary(ctx, environment)
	if err != nil {
		return soak.Evaluation{}, fmt.Errorf("open canary: %w", err)
	}
	defer canary.Close()

	participants := make([]soak.Endpoints, 0, len(environment.Participants))
	for _, participant := range environment.Participants {
		participants = append(participants, soak.Endpoints{
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

	sampler := &soak.Sampler{
		Participants:  participants,
		Thresholds:    thresholds,
		Interval:      runner.configuration.Interval,
		Kube:          kube,
		Canary:        canary,
		SlotsPerEpoch: defaultSlots,
		Out:           samplesFile,
		Log:           log.New(runner.stderr, "soak: ", 0),
	}
	samples, err := sampler.Run(ctx, runner.configuration.Duration)
	if err != nil {
		return soak.Evaluation{}, fmt.Errorf("sample the soak window: %w", err)
	}
	if len(samples) == 0 {
		return soak.Evaluation{}, errors.New("the sampler produced no samples")
	}

	return soak.Evaluate(samples, thresholds, soak.Options{
		Participants:  len(participants),
		SlotsPerEpoch: defaultSlots,
		Enforce:       runner.configuration.Enforce,
		LoadPercent:   runner.configuration.LoadPercent,
		Placement:     placement,
	}), nil
}

func (runner *Runner) writeReports(reportDir string, evaluation soak.Evaluation) error {
	if err := jsonfile.Write(filepath.Join(reportDir, ResultsFile), evaluation, "soak results"); err != nil {
		return err
	}
	if err := jsonfile.Write(filepath.Join(reportDir, VerdictFile), Verdict{
		Passed:   evaluation.Passed,
		Enforced: evaluation.Enforced,
		Class:    VerdictClass(evaluation),
	}, "soak verdict"); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(reportDir, SummaryFile), []byte(RenderSummary(evaluation)), 0o600); err != nil {
		return fmt.Errorf("write soak summary: %w", err)
	}
	return nil
}
