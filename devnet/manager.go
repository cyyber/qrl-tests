// Package devnet starts, inspects and stops Kurtosis-backed QRL development
// networks.
package devnet

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cyyber/qrl-tests/devnet/internal/kurtosis"
	"github.com/cyyber/qrl-tests/internal/devwallet"
	"github.com/cyyber/qrl-tests/internal/dockerapi"
	dockerclient "github.com/moby/moby/client"
)

const (
	DefaultEnclaveName  = "go-qrl-devnet"
	DefaultStartTimeout = 5 * time.Minute

	startCleanupTimeout        = time.Minute     // destroy call after a failed start
	destroyConfirmationTimeout = time.Minute     // confirm loop in destroyAndConfirm
	startDiagnosticsTimeout    = 2 * time.Minute // inspection and logs before a failed start is cleaned up
	retryInterval              = 500 * time.Millisecond

	// PackageLocator pins the qrl-package revision every network runs.
	PackageLocator = "github.com/cyyber/qrl-package@514f0835097b60f2485b8a64f62fb6ecf8bae087"
)

// enclaveClient owns normal enclave and package operations through the
// Kurtosis SDK.
type enclaveClient interface {
	EnclaveExists(ctx context.Context, name string) (bool, error)
	CreateEnclave(ctx context.Context, name string) error
	RunRemotePackage(ctx context.Context, enclaveName, locator, serializedParams string) error
	Services(ctx context.Context, enclaveName string) (map[string]kurtosis.Service, error)
	StopServices(ctx context.Context, enclaveName string, names []string) error
	StartServices(ctx context.Context, enclaveName string, names []string) error
	DestroyEnclave(ctx context.Context, name string) error
}

type containerKiller interface {
	containerLister
	ContainerKill(context.Context, string, dockerclient.ContainerKillOptions) (dockerclient.ContainerKillResult, error)
	ContainerInspect(context.Context, string, dockerclient.ContainerInspectOptions) (dockerclient.ContainerInspectResult, error)
}

// KilledService is a service KillServices killed, with when its container
// exited on the Docker host's clock.
type KilledService struct {
	Name     string
	ExitedAt time.Time
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
	newEnclaveClient     func() (enclaveClient, error)
	newDiagnosticsClient func() (diagnosticsClient, error)
	probe                func(ctx context.Context, rpcURL, address string) error
}

func NewManager() *Manager {
	return &Manager{
		newEnclaveClient: func() (enclaveClient, error) {
			client, err := kurtosis.NewEnclaveClient()
			if err != nil {
				return nil, fmt.Errorf("connect to Kurtosis engine: %w", err)
			}
			return client, nil
		},
		newDiagnosticsClient: func() (diagnosticsClient, error) {
			client, err := kurtosis.NewDiagnosticsClient()
			if err != nil {
				return nil, fmt.Errorf("connect to Kurtosis diagnostics API: %w", err)
			}
			return client, nil
		},
		probe: probeNetwork,
	}
}

// Inspect resolves the services of a running network. It does not wait for
// the network; CheckReady checks that it is ready.
func (manager *Manager) Inspect(ctx context.Context, name string) (Environment, error) {
	client, err := manager.newEnclaveClient()
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
	return resolveEnvironment(ctx, client, name)
}

// CheckReady checks that the chain of the network advances and that the
// development wallet is funded.
func (manager *Manager) CheckReady(ctx context.Context, environment Environment) error {
	primary, err := environment.Primary()
	if err != nil {
		return err
	}
	return manager.probe(ctx, primary.Execution.RPCURL, devwallet.Address)
}

func (manager *Manager) Start(ctx context.Context, options StartOptions) (environment Environment, err error) {
	options.EnclaveName = cmp.Or(options.EnclaveName, DefaultEnclaveName)
	options.Backend = cmp.Or(options.Backend, BackendDocker)
	options.Profile = cmp.Or(options.Profile, ProfileSingle)

	parameters, err := resolveParameters(devwallet.Address, options)
	if err != nil {
		return Environment{}, fmt.Errorf("prepare qrl-package parameters: %w", err)
	}

	client, err := manager.newEnclaveClient()
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

// finishFailedStart runs after any failure that follows enclave creation. It
// collects the requested diagnostics and then destroys the partially
// provisioned network. Diagnostics and cleanup problems are reported alongside
// the start failure, never instead of it.
func (manager *Manager) finishFailedStart(client enclaveClient, options StartOptions, failure error) error {
	// Diagnostics and cleanup run on fresh contexts: the start context is
	// typically already canceled or expired by the time the failure gets here.
	var diagnosticsErr error
	if options.FailureDiagnosticsDir != "" {
		collectCtx, cancel := context.WithTimeout(context.Background(), startDiagnosticsTimeout)
		if err := manager.CollectDiagnostics(collectCtx, options.EnclaveName, options.FailureDiagnosticsDir); err != nil {
			diagnosticsErr = fmt.Errorf("collect start diagnostics: %w", err)
		}
		cancel()
	}

	cleanupCtx, cancel := context.WithTimeout(context.Background(), startCleanupTimeout)
	cleanupErr := manager.destroyAndConfirm(cleanupCtx, client, options.EnclaveName)
	cancel()
	if cleanupErr != nil {
		cleanupErr = fmt.Errorf("clean up failed network: %w", cleanupErr)
	}
	if diagnosticsErr == nil && cleanupErr == nil {
		return failure
	}
	return errors.Join(failure, diagnosticsErr, cleanupErr)
}

func (manager *Manager) Stop(ctx context.Context, name string) error {
	client, err := manager.newEnclaveClient()
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

// StopServices stops the named services of a running network, in order. They
// keep their data and flags until StartServices starts them again.
func (manager *Manager) StopServices(ctx context.Context, name string, services []string) error {
	client, err := manager.newEnclaveClient()
	if err != nil {
		return err
	}
	if err := client.StopServices(ctx, name, services); err != nil {
		return fmt.Errorf("stop services %v: %w", services, err)
	}
	return nil
}

// KillServices kills the named services of a running network at once,
// without letting them shut down. They keep their data and flags until
// StartServices starts them again. Once every container is found and right
// before the kills, it calls ready, if set, and kills nothing if that fails:
// a caller uses it to pick the moment the kills land. It reports when each
// service exited.
func (manager *Manager) KillServices(
	ctx context.Context, name string, services []string, ready func(context.Context) error,
) ([]KilledService, error) {
	docker, err := dockerapi.New()
	if err != nil {
		return nil, fmt.Errorf("kill services %v: create Docker client: %w", services, err)
	}
	defer func() { _ = docker.Close() }()

	killed, err := manager.killServices(ctx, docker, name, services, ready)
	if err != nil {
		return nil, fmt.Errorf("kill services %v: %w", services, err)
	}
	return killed, nil
}

func (manager *Manager) killServices(
	ctx context.Context, docker containerKiller, name string, services []string, ready func(context.Context) error,
) ([]KilledService, error) {
	client, err := manager.newEnclaveClient()
	if err != nil {
		return nil, err
	}
	running, err := client.Services(ctx, name)
	if err != nil {
		return nil, err
	}
	containers, err := serviceContainers(ctx, docker, name, running, services)
	if err != nil {
		return nil, err
	}
	if ready != nil {
		if err := ready(ctx); err != nil {
			return nil, err
		}
	}

	killed, err := killContainers(ctx, docker, services, containers)

	// Kurtosis still records the killed services as running, and it only
	// starts services it stopped. Stopping a container that has already
	// exited does nothing else. Mark them stopped even if another kill
	// failed, so they can still be started again.
	if len(killed) > 0 {
		names := make([]string, len(killed))
		for index, service := range killed {
			names[index] = service.Name
		}
		if stopErr := client.StopServices(ctx, name, names); stopErr != nil {
			return nil, errors.Join(err, stopErr)
		}
	}
	if err != nil {
		return nil, err
	}
	return killed, nil
}

// serviceContainers finds the container of each service, in order, so that
// the kills that follow need no further lookup.
func serviceContainers(
	ctx context.Context, docker containerLister, name string, running map[string]kurtosis.Service, services []string,
) ([]string, error) {
	containers := make([]string, len(services))
	for index, service := range services {
		info, found := running[service]
		if !found {
			return nil, fmt.Errorf("network %q has no service %q", name, service)
		}
		container, err := serviceContainer(ctx, docker, info.UUID)
		if err != nil {
			return nil, fmt.Errorf("find %s container: %w", service, err)
		}
		containers[index] = container.ID
	}
	return containers, nil
}

// killContainers kills the container of every service at once, as a power
// loss would, and returns the services it killed, in the given order, with
// the errors of the kills that failed. Docker reports a kill once the
// container has exited; the exit time it recorded is kept, as the kills of
// several containers land some time apart.
func killContainers(ctx context.Context, docker containerKiller, services, containers []string) ([]KilledService, error) {
	exits := make([]time.Time, len(services))
	killErrs := make([]error, len(services))
	inspectErrs := make([]error, len(services))
	var kills sync.WaitGroup
	for index, service := range services {
		kills.Go(func() {
			if _, err := docker.ContainerKill(ctx, containers[index], dockerclient.ContainerKillOptions{Signal: "SIGKILL"}); err != nil {
				killErrs[index] = fmt.Errorf("kill %s: %w", service, err)
				return
			}
			if exitedAt, err := containerExit(ctx, docker, containers[index]); err != nil {
				inspectErrs[index] = fmt.Errorf("inspect killed %s: %w", service, err)
			} else {
				exits[index] = exitedAt
			}
		})
	}
	kills.Wait()

	var killed []KilledService
	for index, service := range services {
		if killErrs[index] == nil {
			killed = append(killed, KilledService{Name: service, ExitedAt: exits[index]})
		}
	}
	return killed, errors.Join(errors.Join(killErrs...), errors.Join(inspectErrs...))
}

func containerExit(ctx context.Context, docker containerKiller, container string) (time.Time, error) {
	inspection, err := docker.ContainerInspect(ctx, container, dockerclient.ContainerInspectOptions{})
	if err != nil {
		return time.Time{}, err
	}
	if inspection.Container.State == nil {
		return time.Time{}, errors.New("no container state")
	}
	exitedAt, err := time.Parse(time.RFC3339Nano, inspection.Container.State.FinishedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse exit time: %w", err)
	}
	return exitedAt, nil
}

// StartServices starts the named stopped services, in order. They come back
// on new host ports, so the environment has to be inspected again.
func (manager *Manager) StartServices(ctx context.Context, name string, services []string) error {
	client, err := manager.newEnclaveClient()
	if err != nil {
		return err
	}
	if err := client.StartServices(ctx, name, services); err != nil {
		return fmt.Errorf("start services %v: %w", services, err)
	}
	return nil
}

func (manager *Manager) destroyAndConfirm(ctx context.Context, client enclaveClient, name string) error {
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
