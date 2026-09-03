// Package soak provisions a soak-profile network, samples it, evaluates
// versioned gates, and writes a verdict. qrltest soak is the command.
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
	"github.com/cyyber/qrl-tests/internal/runmanifest"
)

const (
	DefaultReportDir    = "reports"
	DefaultDuration     = 4 * time.Hour
	DefaultInterval     = 30 * time.Second
	DefaultStartTimeout = 20 * time.Minute

	SamplesFile    = "samples.jsonl"
	ResultsFile    = "results.json"
	VerdictFile    = "verdict.json"
	SummaryFile    = "summary.md"
	ComparisonFile = "comparison.json"
	ManifestFile   = "manifest.json"
	OutputLog      = "output.log"

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
	LoadPercent      int
	ParticipantCount int
	StartTimeout     time.Duration
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

type sampleFunc func(ctx context.Context, environment devnet.Environment, reportDir string) (Evaluation, error)

type Runner struct {
	configuration Config
	networks      networkManager
	sample        sampleFunc
	progress      ProgressReporter
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

	if runner.progress == nil {
		if progress := jobProgress(); progress != nil {
			runner.progress = progress
		}
	}
	if err := reportPhase(ctx, runner.progress, "provisioning"); err != nil {
		fmt.Fprintf(runner.stderr, "report progress: %v\n", err)
	}

	record := runner.initialRunManifest(ctx)
	if err := record.Write(filepath.Join(reportDir, runmanifest.FileName)); err != nil {
		return err
	}

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
	runner.attachProvenance(&evaluation, record)
	if err = runner.writeReports(reportDir, evaluation); err != nil {
		return err
	}
	record.Finish(map[string]bool{"soak": evaluation.Passed}, time.Now())
	if err := record.Write(filepath.Join(reportDir, runmanifest.FileName)); err != nil {
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
		ParticipantCount:      runner.configuration.ParticipantCount,
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

func (runner *Runner) sampleNetwork(ctx context.Context, environment devnet.Environment, reportDir string) (Evaluation, error) {
	thresholds, err := LoadThresholds(runner.configuration.ThresholdsPath)
	if err != nil {
		return Evaluation{}, err
	}

	var kube *Kube
	if environment.Backend == devnet.BackendKubernetes {
		kube, err = InClusterKube(environment.Namespace())
		if err != nil {
			fmt.Fprintf(runner.stderr, "in-cluster kube unavailable: %v\n", err)
			kube = nil
		}
	}

	var placement []Placement
	if kube != nil {
		placement, err = VerifyPlacement(ctx, kube, devnet.ParticipantNodeLabel, len(environment.Participants))
		if err != nil {
			return Evaluation{}, fmt.Errorf("placement: %w", err)
		}
	}

	samplesPath := filepath.Join(reportDir, SamplesFile)
	samplesFile, err := os.OpenFile(samplesPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return Evaluation{}, fmt.Errorf("create samples file: %w", err)
	}
	defer samplesFile.Close()

	canary, err := newCanary(ctx, environment)
	if err != nil {
		return Evaluation{}, fmt.Errorf("open canary: %w", err)
	}
	defer canary.Close()

	participants := make([]Endpoints, 0, len(environment.Participants))
	for _, participant := range environment.Participants {
		participants = append(participants, Endpoints{
			Index:        participant.Index,
			ExecutionRPC: participant.Execution.RPCURL,
			ConsensusAPI: participant.Consensus.URL,
			Metrics: map[Client]string{
				ClientExecution: participant.Execution.MetricsURL,
				ClientConsensus: participant.Consensus.MetricsURL,
				ClientValidator: participant.Validator.MetricsURL,
			},
		})
	}

	sampler := &Sampler{
		Participants:  participants,
		Thresholds:    thresholds,
		Interval:      runner.configuration.Interval,
		Kube:          kube,
		Canary:        canary,
		SlotsPerEpoch: defaultSlots,
		Out:           samplesFile,
		Log:           log.New(runner.stderr, "soak: ", 0),
		Progress:      runner.progress,
	}
	samples, err := sampler.Run(ctx, runner.configuration.Duration)
	if err != nil {
		return Evaluation{}, fmt.Errorf("sample the soak window: %w", err)
	}
	if len(samples) == 0 {
		return Evaluation{}, errors.New("the sampler produced no samples")
	}

	return Evaluate(samples, thresholds, Options{
		Participants:  len(participants),
		SlotsPerEpoch: defaultSlots,
		Enforce:       runner.configuration.Enforce,
		LoadPercent:   runner.configuration.LoadPercent,
		Placement:     placement,
	}), nil
}

func (runner *Runner) writeReports(reportDir string, evaluation Evaluation) error {
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

func (runner *Runner) initialRunManifest(ctx context.Context) runmanifest.Manifest {
	images := runner.configuration.Images
	if resolved, err := images.Resolved(); err == nil {
		images = resolved
	}
	testsDir := cmp.Or(os.Getenv("QRL_TESTS_SOURCE_DIR"), ".")
	return runmanifest.Enrich(ctx, testsDir, runmanifest.Manifest{
		Images:         &images,
		PackageLocator: devnet.PackageLocator,
		Backend:        runner.configuration.Backend,
		Lanes: []runmanifest.Lane{{
			Name:    "soak",
			Enclave: runner.configuration.EnclaveName,
			Profile: devnet.ProfileSoak,
		}},
	})
}

func (runner *Runner) attachProvenance(evaluation *Evaluation, record runmanifest.Manifest) {
	evaluation.QRLTests = record.Sources.QRLTests
	evaluation.PackageLocator = record.PackageLocator
	if record.Images != nil {
		evaluation.Images = imageMap(*record.Images)
	}
}

func imageMap(images devnet.Images) map[string]string {
	return map[string]string{
		"execution":        images.Execution,
		"clef":             images.Clef,
		"consensus":        images.Consensus,
		"validator":        images.Validator,
		"genesis":          images.Genesis,
		"tx_spammer":       images.TxSpammer,
		"metrics_exporter": images.MetricsExporter,
	}
}

func jobProgress() *JobProgress {
	name, namespace := os.Getenv("JOB_NAME"), os.Getenv("JOB_NAMESPACE")
	if name == "" || namespace == "" {
		return nil
	}
	kube, err := InClusterKube(namespace)
	if err != nil {
		return nil
	}
	return &JobProgress{Kube: kube, Name: name}
}

func reportPhase(ctx context.Context, progress ProgressReporter, phase string) error {
	job, ok := progress.(*JobProgress)
	if !ok {
		return nil
	}
	return job.reportPhase(ctx, phase)
}
