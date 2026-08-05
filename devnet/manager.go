// Package devnet controls separately managed QRL development networks.
package devnet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cyyber/qrl-tests/devnet/internal/kurtosis"
	"github.com/cyyber/qrl-tests/internal/fixture"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/qrlclient"
)

type kurtosisClient interface {
	EnclaveExists(context.Context, string) (bool, error)
	CreateAndRunRemotePackage(context.Context, string, string, string) (bool, error)
	Services(context.Context, string) (map[string]kurtosis.Service, error)
	StartServices(context.Context, string, ...string) error
	StopServices(context.Context, string, ...string) error
	DestroyEnclave(context.Context, string) error
}

const (
	DefaultEnclaveName  = "go-qrl-devnet"
	DefaultStartTimeout = 5 * time.Minute

	destroyConfirmationTimeout = 2 * time.Minute
	retryInterval              = 500 * time.Millisecond
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
	if err := manager.probe(ctx, primary.Execution.RPCURL, fixture.DevelopmentWalletAddress); err != nil {
		return Environment{}, err
	}
	return environment, nil
}

func (manager *Manager) Start(ctx context.Context, options StartOptions) (Environment, error) {
	backend, err := ParseBackend(string(options.Backend))
	if err != nil {
		return Environment{}, err
	}
	parameters, err := effectiveParametersForProfile(
		fixture.DevelopmentWalletAddress,
		options.Images,
		options.Parameters,
		options.Profile,
		backend,
	)
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
	created, err := client.CreateAndRunRemotePackage(
		ctx,
		options.EnclaveName,
		packageLocator,
		parameters,
	)
	if err != nil {
		return Environment{}, manager.startFailure(client, options.EnclaveName, created, "create enclave or run pinned qrl-package", err)
	}

	// Endpoints are fixed once the package run completes; only the probe has to
	// wait for the chain to come up.
	environment, err := resolveEnvironment(ctx, client, options.EnclaveName, backend)
	if err != nil {
		return Environment{}, manager.startFailure(client, options.EnclaveName, true, "resolve network endpoints", err)
	}
	primary, err := environment.Primary()
	if err != nil {
		return Environment{}, manager.startFailure(client, options.EnclaveName, true, "resolve primary participant", err)
	}
	if err := retryUntil(ctx, func() error {
		return manager.probe(ctx, primary.Execution.RPCURL, fixture.DevelopmentWalletAddress)
	}); err != nil {
		return Environment{}, manager.startFailure(client, options.EnclaveName, true, "wait for network readiness", err)
	}
	return environment, nil
}

func (manager *Manager) startFailure(client kurtosisClient, name string, created bool, operation string, failure error) error {
	result := fmt.Errorf("%s: %w", operation, failure)
	if !created {
		return result
	}
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

const chainAdvancementWindow = 30 * time.Second

func probeNetwork(ctx context.Context, rpcURL, address string) error {
	client, err := qrlclient.DialContext(ctx, rpcURL)
	if err != nil {
		return fmt.Errorf("dial execution RPC: %w", err)
	}
	defer client.Close()

	firstBlock, err := client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("read block number: %w", err)
	}

	advancementCtx, cancel := context.WithTimeout(ctx, chainAdvancementWindow)
	defer cancel()
	if err := retryUntil(advancementCtx, func() error {
		block, err := client.BlockNumber(advancementCtx)
		if err != nil {
			return fmt.Errorf("read advancing block number: %w", err)
		}
		if block <= firstBlock {
			return fmt.Errorf("block number remains at %d", block)
		}
		return nil
	}); err != nil {
		return fmt.Errorf(
			"chain did not advance beyond block %d within %s: %w",
			firstBlock,
			chainAdvancementWindow,
			err,
		)
	}

	account, err := common.NewAddressFromString(address)
	if err != nil {
		return fmt.Errorf("parse development wallet address: %w", err)
	}
	balance, err := client.BalanceAt(ctx, account, nil)
	if err != nil {
		return fmt.Errorf("read development wallet balance: %w", err)
	}
	if balance.Sign() <= 0 {
		return fmt.Errorf("development wallet %s has no balance", address)
	}

	return nil
}
