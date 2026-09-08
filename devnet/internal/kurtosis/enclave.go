// Package kurtosis provides the narrow Kurtosis API used by the development
// network controller. It converts SDK types into local ones at the boundary
// so Kurtosis internals never leak into devnet.
package kurtosis

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kurtosis-tech/kurtosis/api/golang/core/kurtosis_core_rpc_api_bindings"
	"github.com/kurtosis-tech/kurtosis/api/golang/core/lib/services"
	"github.com/kurtosis-tech/kurtosis/api/golang/core/lib/starlark_run_config"
	"github.com/kurtosis-tech/kurtosis/api/golang/engine/lib/kurtosis_context"
)

type Service struct {
	UUID         string
	PrivateIP    string
	PublicIP     string
	PublicPorts  map[string]uint16
	PrivatePorts map[string]uint16
	Labels       map[string]string
}

func (service Service) PublicEndpoint(portID, scheme string) (string, error) {
	port := service.PublicPorts[portID]
	if port == 0 {
		return "", fmt.Errorf("no public %q port", portID)
	}
	if service.PublicIP == "" {
		return "", errors.New("no public IP address")
	}
	return scheme + "://" + net.JoinHostPort(service.PublicIP, strconv.Itoa(int(port))), nil
}

// PrivateEndpoint addresses the service inside the enclave network: the
// container address on Docker, the Kubernetes Service address on Kubernetes.
func (service Service) PrivateEndpoint(portID, scheme string) (string, error) {
	port := service.PrivatePorts[portID]
	if port == 0 {
		return "", fmt.Errorf("no private %q port", portID)
	}
	if service.PrivateIP == "" {
		return "", errors.New("no private IP address")
	}
	return scheme + "://" + net.JoinHostPort(service.PrivateIP, strconv.Itoa(int(port))), nil
}

// EnclaveClient manages enclaves and packages through the Kurtosis SDK.
type EnclaveClient struct {
	engine *kurtosis_context.KurtosisContext
}

// NewEnclaveClient connects to the local Kurtosis engine through its SDK.
func NewEnclaveClient() (*EnclaveClient, error) {
	engine, err := kurtosis_context.NewKurtosisContextFromLocalEngine()
	if err != nil {
		return nil, err
	}
	return &EnclaveClient{engine: engine}, nil
}

func (client *EnclaveClient) EnclaveExists(ctx context.Context, name string) (bool, error) {
	running, err := client.engine.GetEnclaves(ctx)
	if err != nil {
		return false, fmt.Errorf("list running Kurtosis enclaves: %w", err)
	}
	_, found := running.GetEnclavesByName()[name]
	return found, nil
}

func (client *EnclaveClient) CreateEnclave(ctx context.Context, name string) error {
	_, err := client.engine.CreateEnclave(ctx, name)
	return err
}

func (client *EnclaveClient) RunRemotePackage(
	ctx context.Context,
	enclaveName string,
	locator,
	serializedParams string,
) error {
	enclave, err := client.engine.GetEnclaveContext(ctx, enclaveName)
	if err != nil {
		return err
	}

	configuration := starlark_run_config.NewRunStarlarkConfig(starlark_run_config.WithSerializedParams(serializedParams))
	stream, cancel, err := enclave.RunStarlarkRemotePackage(ctx, locator, configuration)
	if err != nil {
		return err
	}
	defer cancel()

	// qrl-package output can contain generated seed material. Completion is all
	// the network controller needs, so raw serialized output never escapes this
	// SDK boundary.
	return consumeStarlarkCompletion(stream)
}

func (client *EnclaveClient) Services(ctx context.Context, enclaveName string) (map[string]Service, error) {
	var last error
	for attempt := 0; attempt < 30; attempt++ {
		result, err := client.servicesOnce(ctx, enclaveName)
		if err == nil && len(result) > 0 {
			return result, nil
		}
		if err == nil {
			err = errors.New("enclave listed no services")
		}
		last = err
		if ctx.Err() != nil {
			return nil, errors.Join(last, ctx.Err())
		}
		if !transientKurtosisError(err) {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "kurtosis: retrying service listing (%s)\n", err)
		select {
		case <-ctx.Done():
			return nil, errors.Join(last, ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
	return nil, last
}

func (client *EnclaveClient) servicesOnce(ctx context.Context, enclaveName string) (map[string]Service, error) {
	enclave, err := client.engine.GetEnclaveContext(ctx, enclaveName)
	if err != nil {
		return nil, err
	}
	identifiers, err := enclave.GetServices()
	if err != nil {
		return nil, err
	}

	wanted := make(map[string]bool, len(identifiers))
	for name := range identifiers {
		wanted[string(name)] = true
	}
	contexts, err := enclave.GetServiceContexts(wanted)
	if err != nil {
		return nil, err
	}

	result := make(map[string]Service, len(contexts))
	for name, serviceCtx := range contexts {
		result[string(name)] = newService(serviceCtx)
	}
	return result, nil
}

func transientKurtosisError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "Unavailable") ||
		strings.Contains(message, "EOF") ||
		strings.Contains(message, "connection refused") ||
		strings.Contains(message, "error reading from server") ||
		strings.Contains(message, "enclave listed no services")
}

func (client *EnclaveClient) DestroyEnclave(ctx context.Context, name string) error {
	return client.engine.DestroyEnclave(ctx, name)
}

type serviceContext interface {
	GetServiceUUID() services.ServiceUUID
	GetPrivateIPAddress() string
	GetMaybePublicIPAddress() string
	GetPublicPorts() map[string]*services.PortSpec
	GetPrivatePorts() map[string]*services.PortSpec
	GetLabels() map[string]string
}

func newService(source serviceContext) Service {
	return Service{
		UUID:         string(source.GetServiceUUID()),
		PrivateIP:    source.GetPrivateIPAddress(),
		PublicIP:     source.GetMaybePublicIPAddress(),
		PublicPorts:  portNumbers(source.GetPublicPorts()),
		PrivatePorts: portNumbers(source.GetPrivatePorts()),
		Labels:       maps.Clone(source.GetLabels()),
	}
}

func portNumbers(specs map[string]*services.PortSpec) map[string]uint16 {
	numbers := make(map[string]uint16, len(specs))
	for id, port := range specs {
		numbers[id] = port.GetNumber()
	}
	return numbers
}

func consumeStarlarkCompletion(stream <-chan *kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine) error {
	var runErr error
	for line := range stream {
		if progress := line.GetProgressInfo(); progress != nil {
			steps := progress.GetCurrentStepInfo()
			if n := len(steps); n > 0 && steps[n-1] != "" {
				fmt.Fprintf(os.Stderr, "kurtosis: %s\n", steps[n-1])
			}
		}
		if responseErr := line.GetError(); responseErr != nil {
			runErr = errors.Join(runErr, starlarkError(responseErr))
		}
		if finished := line.GetRunFinishedEvent(); finished != nil {
			if !finished.GetIsRunSuccessful() {
				if runErr != nil {
					return runErr
				}
				return errors.New("Kurtosis Starlark package run failed without a structured error")
			}
			return nil
		}
	}
	if runErr != nil {
		return runErr
	}
	return errors.New("Kurtosis Starlark response stream closed without a terminal event")
}

func starlarkError(responseErr *kurtosis_core_rpc_api_bindings.StarlarkError) error {
	if detail := responseErr.GetInterpretationError(); detail != nil {
		return fmt.Errorf("Kurtosis Starlark interpretation failed: %s", detail.GetErrorMessage())
	}
	if detail := responseErr.GetValidationError(); detail != nil {
		return fmt.Errorf("Kurtosis Starlark validation failed: %s", detail.GetErrorMessage())
	}
	if detail := responseErr.GetExecutionError(); detail != nil {
		return fmt.Errorf("Kurtosis Starlark execution failed: %s", detail.GetErrorMessage())
	}
	return errors.New("Kurtosis Starlark package run failed with an unknown structured error")
}
