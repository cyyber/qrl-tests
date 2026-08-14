// Package devnet starts, inspects and stops Kurtosis-backed QRL development
// networks.
package devnet

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/cyyber/qrl-tests/devnet/internal/kurtosis"
	"github.com/cyyber/qrl-tests/internal/devwallet"
)

const (
	DefaultEnclaveName  = "go-qrl-devnet"
	DefaultStartTimeout = 5 * time.Minute

	startCleanupTimeout        = time.Minute     // destroy call after a failed start
	destroyConfirmationTimeout = time.Minute     // confirm loop in destroyAndConfirm
	startDiagnosticsTimeout    = 2 * time.Minute // inspection and logs before a failed start is cleaned up
	retryInterval              = 500 * time.Millisecond

	// PackageLocator pins the qrl-package revision every network runs.
	PackageLocator = "github.com/cyyber/qrl-package@bcb7370dc416606a2982d1f9192073232281154f"
)

type kurtosisClient interface {
	EnclaveExists(ctx context.Context, name string) (bool, error)
	CreateEnclave(ctx context.Context, name string) error
	RunRemotePackage(ctx context.Context, enclaveName, locator, serializedParams string) error
	Services(ctx context.Context, enclaveName string) (map[string]kurtosis.Service, error)
	DestroyEnclave(ctx context.Context, name string) error
}

type StartOptions struct {
	EnclaveName string
	Backend     Backend
	Images      Images
	Parameters  []byte
	Profile     Profile

	// FailureDiagnosticsDir, when set, receives the enclave's diagnostics
	// before cleanup of a failed start is attempted.
	FailureDiagnosticsDir string
}

type Manager struct {
	newClient          func() (kurtosisClient, error)
	probe              func(ctx context.Context, rpcURL, address string) error
	collectDiagnostics func(ctx context.Context, enclave, outputDir string) error
}

func NewManager() *Manager {
	runDiagnostics := func(ctx context.Context, name string, arguments ...string) (string, error) {
		output, err := exec.CommandContext(ctx, name, arguments...).CombinedOutput()
		return string(output), err
	}

	return &Manager{
		newClient: func() (kurtosisClient, error) {
			client, err := kurtosis.NewClient()
			if err != nil {
				return nil, fmt.Errorf("connect to Kurtosis engine: %w", err)
			}
			return client, nil
		},
		probe: probeNetwork,
		collectDiagnostics: func(ctx context.Context, enclave, outputDir string) error {
			return collectDiagnostics(ctx, runDiagnostics, enclave, outputDir)
		},
	}
}

func (manager *Manager) Inspect(ctx context.Context, name string) (Environment, error) {
	client, err := manager.newClient()
	if err != nil {
		return Environment{}, err
	}
	found, err := client.EnclaveExists(ctx, name)
	if err != nil {
		return Environment{}, err
	}
	if !found {
		return Environment{}, fmt.Errorf("network %q is not running", name)
	}

	environment, err := resolveEnvironment(ctx, client, name)
	if err != nil {
		return Environment{}, err
	}

	primary, err := environment.Primary()
	if err != nil {
		return Environment{}, err
	}
	if err := manager.probe(ctx, primary.Execution.RPCURL, devwallet.Address); err != nil {
		return Environment{}, err
	}

	return environment, nil
}

func (manager *Manager) Start(ctx context.Context, options StartOptions) (environment Environment, err error) {
	options = resolveStartOptions(options)

	parameters, err := resolveParameters(devwallet.Address, options)
	if err != nil {
		return Environment{}, fmt.Errorf("prepare qrl-package parameters: %w", err)
	}

	client, err := manager.newClient()
	if err != nil {
		return Environment{}, err
	}
	found, err := client.EnclaveExists(ctx, options.EnclaveName)
	if err != nil {
		return Environment{}, err
	}
	if found {
		return Environment{}, fmt.Errorf("network %q already exists or provisioning is incomplete; stop it before retrying", options.EnclaveName)
	}

	if err := client.CreateEnclave(ctx, options.EnclaveName); err != nil {
		return Environment{}, fmt.Errorf("create enclave: %w", err)
	}
	defer func() {
		if err != nil {
			err = manager.finishFailedStart(client, options, err)
		}
	}()

	if err := client.RunRemotePackage(ctx, options.EnclaveName, PackageLocator, parameters); err != nil {
		return Environment{}, fmt.Errorf("run pinned qrl-package: %w", err)
	}

	// Endpoints are fixed once the package run completes; only the probe has to
	// wait for the chain to come up.
	environment, err = resolveEnvironment(ctx, client, options.EnclaveName)
	if err != nil {
		return Environment{}, fmt.Errorf("resolve network endpoints: %w", err)
	}
	environment.Backend = options.Backend

	primary, err := environment.Primary()
	if err != nil {
		return Environment{}, fmt.Errorf("resolve primary participant: %w", err)
	}
	if err := retryUntil(ctx, func() error {
		return manager.probe(ctx, primary.Execution.RPCURL, devwallet.Address)
	}); err != nil {
		return Environment{}, fmt.Errorf("wait for network readiness: %w", err)
	}

	return environment, nil
}

func resolveStartOptions(options StartOptions) StartOptions {
	options.EnclaveName = cmp.Or(options.EnclaveName, DefaultEnclaveName)
	options.Backend = cmp.Or(options.Backend, BackendDocker)
	options.Profile = cmp.Or(options.Profile, ProfileSingle)
	return options
}

// finishFailedStart runs after any failure that follows enclave creation. It
// collects the requested diagnostics and then destroys the partially
// provisioned network. Diagnostics and cleanup problems are reported alongside
// the start failure, never instead of it.
func (manager *Manager) finishFailedStart(client kurtosisClient, options StartOptions, failure error) error {
	result := failure

	// Diagnostics and cleanup run on fresh contexts: the start context is
	// typically already canceled or expired by the time the failure gets here.
	if options.FailureDiagnosticsDir != "" {
		collectCtx, cancel := context.WithTimeout(context.Background(), startDiagnosticsTimeout)
		if err := manager.collectDiagnostics(collectCtx, options.EnclaveName, options.FailureDiagnosticsDir); err != nil {
			result = errors.Join(result, fmt.Errorf("collect start diagnostics: %w", err))
		}
		cancel()
	}

	cleanupCtx, cancel := context.WithTimeout(context.Background(), startCleanupTimeout)
	defer cancel()
	if err := manager.destroyAndConfirm(cleanupCtx, client, options.EnclaveName); err != nil {
		return errors.Join(result, fmt.Errorf("clean up failed network: %w", err))
	}
	return result
}

func (manager *Manager) Stop(ctx context.Context, name string) error {
	client, err := manager.newClient()
	if err != nil {
		return err
	}
	found, err := client.EnclaveExists(ctx, name)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	return manager.destroyAndConfirm(ctx, client, name)
}

func (manager *Manager) destroyAndConfirm(ctx context.Context, client kurtosisClient, name string) error {
	destroyErr := client.DestroyEnclave(ctx, name)
	// Confirm the deterministic slot is actually free — on a fresh context so
	// cancellation cannot fake a successful stop — because the next start
	// trusts this result.
	confirmCtx, cancel := context.WithTimeout(context.Background(), destroyConfirmationTimeout)
	defer cancel()
	confirmErr := retryUntil(confirmCtx, func() error {
		found, err := client.EnclaveExists(confirmCtx, name)
		if err != nil {
			return fmt.Errorf("confirm enclave destruction: %w", err)
		}
		if found {
			return errors.New("enclave still occupies its slot")
		}
		return nil
	})
	return errors.Join(destroyErr, confirmErr)
}

func retryUntil(ctx context.Context, operation func() error) error {
	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()

	for {
		err := operation()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		case <-ticker.C:
		}
	}
}
