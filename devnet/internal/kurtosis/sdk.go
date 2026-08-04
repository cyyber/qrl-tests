// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

// Package kurtosis provides the narrow Kurtosis API used by the development
// network controller. Raw SDK types deliberately do not escape this package.
package kurtosis

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"strconv"

	"github.com/kurtosis-tech/kurtosis/api/golang/core/kurtosis_core_rpc_api_bindings"
	"github.com/kurtosis-tech/kurtosis/api/golang/core/lib/enclaves"
	"github.com/kurtosis-tech/kurtosis/api/golang/core/lib/services"
	"github.com/kurtosis-tech/kurtosis/api/golang/core/lib/starlark_run_config"
	"github.com/kurtosis-tech/kurtosis/api/golang/engine/lib/kurtosis_context"
)

type Service struct {
	UUID        string
	PrivateIP   string
	PublicIP    string
	PublicPorts map[string]uint16
	Labels      map[string]string
}

func (service Service) PublicEndpoint(portID, scheme string) (string, error) {
	port, ok := service.PublicPorts[portID]
	if !ok || port == 0 {
		return "", fmt.Errorf("no public %q port", portID)
	}
	if service.PublicIP == "" {
		return "", errors.New("no public IP address")
	}
	return scheme + "://" + net.JoinHostPort(service.PublicIP, strconv.Itoa(int(port))), nil
}

type Client struct {
	context *kurtosis_context.KurtosisContext
}

func NewClient() (*Client, error) {
	ctx, err := kurtosis_context.NewKurtosisContextFromLocalEngine()
	if err != nil {
		return nil, err
	}
	return &Client{context: ctx}, nil
}

func (client *Client) EnclaveExists(ctx context.Context, name string) (bool, error) {
	running, err := client.context.GetEnclaves(ctx)
	if err != nil {
		return false, fmt.Errorf("list running Kurtosis enclaves: %w", err)
	}
	_, found := running.GetEnclavesByName()[name]
	return found, nil
}

func (client *Client) CreateAndRunRemotePackage(
	ctx context.Context,
	name string,
	locator,
	serializedParams string,
) (bool, error) {
	enclave, err := client.context.CreateEnclave(ctx, name)
	if err != nil {
		return false, err
	}
	configuration := starlark_run_config.NewRunStarlarkConfig(starlark_run_config.WithSerializedParams(serializedParams))
	stream, cancel, err := enclave.RunStarlarkRemotePackage(ctx, locator, configuration)
	if err != nil {
		return true, err
	}
	defer cancel()
	// qrl-package output can contain generated seed material. Completion is all
	// the network controller needs, so raw serialized output never escapes this
	// SDK boundary.
	return true, consumeStarlarkCompletion(stream)
}

func (client *Client) Services(ctx context.Context, enclaveName string) (map[string]Service, error) {
	enclave, err := client.enclave(ctx, enclaveName)
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
	for name, context := range contexts {
		result[string(name)] = service(context)
	}
	return result, nil
}

func (client *Client) enclave(ctx context.Context, name string) (*enclaves.EnclaveContext, error) {
	return client.context.GetEnclaveContext(ctx, name)
}

type serviceContext interface {
	GetServiceUUID() services.ServiceUUID
	GetPrivateIPAddress() string
	GetMaybePublicIPAddress() string
	GetPublicPorts() map[string]*services.PortSpec
	GetLabels() map[string]string
}

func service(serviceContext serviceContext) Service {
	publicPorts := make(map[string]uint16, len(serviceContext.GetPublicPorts()))
	for id, port := range serviceContext.GetPublicPorts() {
		publicPorts[id] = port.GetNumber()
	}
	return Service{
		UUID:        string(serviceContext.GetServiceUUID()),
		PrivateIP:   serviceContext.GetPrivateIPAddress(),
		PublicIP:    serviceContext.GetMaybePublicIPAddress(),
		PublicPorts: publicPorts,
		Labels:      maps.Clone(serviceContext.GetLabels()),
	}
}

func (client *Client) DestroyEnclave(ctx context.Context, name string) error {
	return client.context.DestroyEnclave(ctx, name)
}

func consumeStarlarkCompletion(stream <-chan *kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine) error {
	var runErr error
	for line := range stream {
		if responseErr := line.GetError(); responseErr != nil {
			runErr = starlarkError(responseErr)
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
