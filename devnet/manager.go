// Package devnet starts, inspects and stops Kurtosis-backed QRL development
// networks. Networks outlive the processes that use them: one qrltest
// invocation can provision a network and later ones attach to it.
package devnet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cyyber/qrl-tests/devnet/internal/kurtosis"
	"github.com/cyyber/qrl-tests/internal/devwallet"
)

type kurtosisClient interface {
	EnclaveExists(context.Context, string) (bool, error)
	CreateEnclave(context.Context, string) error
	RunRemotePackage(context.Context, string, string, string) error
	Services(context.Context, string) (map[string]kurtosis.Service, error)
	DestroyEnclave(context.Context, string) error
}

const (
	DefaultEnclaveName  = "go-qrl-devnet"
	DefaultStartTimeout = 5 * time.Minute

	destroyConfirmationTimeout = 2 * time.Minute
	retryInterval              = 500 * time.Millisecond

	// packageLocator pins the qrl-package revision run for every network.
	packageLocator = "github.com/rgeraldes24/qrl-package@3892c3d2596403c080424d9e8fc99ff172483fe0"
)

type StartOptions struct {
	EnclaveName string
	Backend     Backend
	Images      Images
	Parameters  []byte
	Profile     Profile
}

type Manager struct {
	newClient func() (kurtosisClient, error)
	probe     func(context.Context, string, string) error
}

func NewManager() *Manager {
	return &Manager{
		newClient: func() (kurtosisClient, error) {
			client, err := kurtosis.NewClient()
			if err != nil {
				return nil, fmt.Errorf("connect to Kurtosis engine: %w", err)
			}
			return client, nil
		},
		probe: probeNetwork,
	}
}

func (manager *Manager) Inspect(ctx context.Context, name string, backend Backend) (Environment, error) {
	backend, err := ParseBackend(string(backend))
	if err != nil {
		return Environment{}, err
	}

	client, err := manager.newClient()
	if err != nil {
		return Environment{}, err
	}
	found, err := client.EnclaveExists(ctx, name)
	if err != nil {
		return Environment{}, err
	}
	if !found {
		return Environment{}, errors.New("network is not running")
	}

	environment, err := resolveEnvironment(ctx, client, name, backend)
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

func (manager *Manager) Start(ctx context.Context, options StartOptions) (Environment, error) {
	backend, err := ParseBackend(string(options.Backend))
	if err != nil {
		return Environment{}, err
	}

	parameters, err := effectiveParameters(devwallet.Address, options)
	if err != nil {
		return Environment{}, fmt.Errorf("prepare qrl-package parameters: %w", err)
	}

	client, err := manager.newClient()
	if err != nil {
		return Environment{}, err
	}
	if found, err := client.EnclaveExists(ctx, options.EnclaveName); err != nil {
		return Environment{}, err
	} else if found {
		return Environment{}, errors.New("network already exists or provisioning is incomplete; stop it before retrying")
	}

	if err := client.CreateEnclave(ctx, options.EnclaveName); err != nil {
		return Environment{}, fmt.Errorf("create enclave: %w", err)
	}
	if err := client.RunRemotePackage(ctx, options.EnclaveName, packageLocator, parameters); err != nil {
		return Environment{}, manager.startFailure(client, options.EnclaveName, "run pinned qrl-package", err)
	}

	// Endpoints are fixed once the package run completes; only the probe has to
	// wait for the chain to come up.
	environment, err := resolveEnvironment(ctx, client, options.EnclaveName, backend)
	if err != nil {
		return Environment{}, manager.startFailure(client, options.EnclaveName, "resolve network endpoints", err)
	}

	primary, err := environment.Primary()
	if err != nil {
		return Environment{}, manager.startFailure(client, options.EnclaveName, "resolve primary participant", err)
	}
	if err := retryUntil(ctx, func() error {
		return manager.probe(ctx, primary.Execution.RPCURL, devwallet.Address)
	}); err != nil {
		return Environment{}, manager.startFailure(client, options.EnclaveName, "wait for network readiness", err)
	}

	return environment, nil
}

// startFailure wraps a failure that happened after the enclave was created,
// destroying the partially provisioned network before returning.
func (manager *Manager) startFailure(client kurtosisClient, name string, operation string, failure error) error {
	result := fmt.Errorf("%s: %w", operation, failure)

	cleanupCtx, cancel := context.WithTimeout(context.Background(), destroyConfirmationTimeout)
	defer cancel()
	if err := manager.destroyAndConfirm(cleanupCtx, client, name); err != nil {
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
