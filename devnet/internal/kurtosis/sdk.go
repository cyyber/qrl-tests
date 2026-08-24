// Package kurtosis provides the narrow Kurtosis API used by the development
// network controller. It converts SDK types into local ones at the boundary
// so Kurtosis internals never leak into devnet.
package kurtosis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kurtosis-tech/kurtosis/api/golang/core/kurtosis_core_rpc_api_bindings"
	"github.com/kurtosis-tech/kurtosis/api/golang/core/lib/services"
	"github.com/kurtosis-tech/kurtosis/api/golang/core/lib/starlark_run_config"
	"github.com/kurtosis-tech/kurtosis/api/golang/engine/kurtosis_engine_rpc_api_bindings"
	"github.com/kurtosis-tech/kurtosis/api/golang/engine/lib/kurtosis_context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

const maxGRPCMessageSize = 100 * 1024 * 1024

type Service struct {
	UUID        string
	PrivateIP   string
	PublicIP    string
	PublicPorts map[string]uint16
	Labels      map[string]string
}

type ServiceIdentity struct {
	Name   string
	UUID   string
	Status string
	Ports  []string
}

type FilesArtifactIdentity struct {
	Name string
	UUID string
}

type EnclaveInspection struct {
	Name           string
	UUID           string
	Status         string
	CreationTime   time.Time
	Production     bool
	Services       []ServiceIdentity
	FilesArtifacts []FilesArtifactIdentity
}

type ServiceLogConsumer func(serviceUUID string, lines []string)

type serviceLogsStream interface {
	Recv() (*kurtosis_engine_rpc_api_bindings.GetServiceLogsResponse, error)
}

type getServiceLogsFunc func(
	ctx context.Context,
	arguments *kurtosis_engine_rpc_api_bindings.GetServiceLogsArgs,
) (serviceLogsStream, error)

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

type Client struct {
	engine         *kurtosis_context.KurtosisContext
	getServiceLogs getServiceLogsFunc
}

func NewClient() (*Client, error) {
	engine, err := kurtosis_context.NewKurtosisContextFromLocalEngine()
	if err != nil {
		return nil, err
	}
	return &Client{engine: engine}, nil
}

func (client *Client) EnclaveExists(ctx context.Context, name string) (bool, error) {
	running, err := client.engine.GetEnclaves(ctx)
	if err != nil {
		return false, fmt.Errorf("list running Kurtosis enclaves: %w", err)
	}
	_, found := running.GetEnclavesByName()[name]
	return found, nil
}

func (client *Client) CreateEnclave(ctx context.Context, name string) error {
	_, err := client.engine.CreateEnclave(ctx, name)
	return err
}

func (client *Client) RunRemotePackage(
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

func (client *Client) Services(ctx context.Context, enclaveName string) (map[string]Service, error) {
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

// Inspect returns enclave metadata and the identifiers needed for diagnostics.
func (client *Client) Inspect(
	ctx context.Context,
	enclaveName string,
) (EnclaveInspection, error) {
	info, err := client.engine.GetEnclave(ctx, enclaveName)
	if err != nil {
		return EnclaveInspection{}, err
	}
	inspection := EnclaveInspection{
		Name:       info.GetName(),
		UUID:       info.GetEnclaveUuid(),
		Status:     strings.TrimPrefix(info.GetContainersStatus().String(), "EnclaveContainersStatus_"),
		Production: info.GetMode() == kurtosis_engine_rpc_api_bindings.EnclaveMode_PRODUCTION,
	}
	var inspectionErrors []error
	if created := info.GetCreationTime(); created != nil {
		inspection.CreationTime = created.AsTime()
	} else {
		inspectionErrors = append(inspectionErrors, errors.New("Kurtosis enclave has no creation time"))
	}
	if info.GetApiContainerStatus() != kurtosis_engine_rpc_api_bindings.EnclaveAPIContainerStatus_EnclaveAPIContainerStatus_RUNNING {
		return inspection, errors.Join(inspectionErrors...)
	}

	inspection.Services, inspection.FilesArtifacts, err = inspectEnclaveContents(ctx, info)
	if err != nil {
		inspectionErrors = append(inspectionErrors, err)
	}
	return inspection, errors.Join(inspectionErrors...)
}

func inspectEnclaveContents(
	ctx context.Context,
	info *kurtosis_engine_rpc_api_bindings.EnclaveInfo,
) ([]ServiceIdentity, []FilesArtifactIdentity, error) {
	host := info.GetApiContainerHostMachineInfo()
	if host == nil || host.GetIpOnHostMachine() == "" || host.GetGrpcPortOnHostMachine() == 0 {
		return nil, nil, errors.New("Kurtosis enclave has no API container endpoint")
	}
	address := net.JoinHostPort(host.GetIpOnHostMachine(), strconv.Itoa(int(host.GetGrpcPortOnHostMachine())))
	connection, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxGRPCMessageSize)),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to Kurtosis API container: %w", err)
	}
	api := kurtosis_core_rpc_api_bindings.NewApiContainerServiceClient(connection)
	historical, historicalErr := api.GetExistingAndHistoricalServiceIdentifiers(ctx, &emptypb.Empty{})
	current, currentErr := api.GetServices(ctx, &kurtosis_core_rpc_api_bindings.GetServicesArgs{
		ServiceIdentifiers: map[string]bool{},
	})
	artifacts, artifactsErr := api.ListFilesArtifactNamesAndUuids(ctx, &emptypb.Empty{})
	closeErr := connection.Close()
	if historicalErr != nil {
		historicalErr = fmt.Errorf("get historical Kurtosis services: %w", historicalErr)
	}
	if currentErr != nil {
		currentErr = fmt.Errorf("get current Kurtosis services: %w", currentErr)
	}
	if artifactsErr != nil {
		artifactsErr = fmt.Errorf("get Kurtosis files artifacts: %w", artifactsErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close Kurtosis API container connection: %w", closeErr)
	}

	servicesByUUID := make(map[string]ServiceIdentity)
	for _, identifier := range historical.GetAllIdentifiers() {
		servicesByUUID[identifier.GetServiceUuid()] = ServiceIdentity{
			Name:   identifier.GetName(),
			UUID:   identifier.GetServiceUuid(),
			Status: "UNKNOWN",
			Ports:  []string{"<unknown>"},
		}
	}
	currentUUIDs := make(map[string]bool, len(current.GetServiceInfo()))
	for _, service := range current.GetServiceInfo() {
		currentUUIDs[service.GetServiceUuid()] = true
		servicesByUUID[service.GetServiceUuid()] = ServiceIdentity{
			Name:   service.GetName(),
			UUID:   service.GetServiceUuid(),
			Status: service.GetContainer().GetStatus().String(),
			Ports:  servicePortBindings(service),
		}
	}
	if currentErr == nil {
		for uuid, service := range servicesByUUID {
			if !currentUUIDs[uuid] {
				service.Status = "HISTORICAL"
				servicesByUUID[uuid] = service
			}
		}
	}
	services := make([]ServiceIdentity, 0, len(servicesByUUID))
	for _, service := range servicesByUUID {
		services = append(services, service)
	}
	sort.Slice(services, func(i, j int) bool {
		if services[i].Name == services[j].Name {
			return services[i].UUID < services[j].UUID
		}
		return services[i].Name < services[j].Name
	})

	files := make([]FilesArtifactIdentity, 0, len(artifacts.GetFileNamesAndUuids()))
	for _, artifact := range artifacts.GetFileNamesAndUuids() {
		files = append(files, FilesArtifactIdentity{
			Name: artifact.GetFileName(),
			UUID: artifact.GetFileUuid(),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})

	return services, files, errors.Join(historicalErr, currentErr, artifactsErr, closeErr)
}

func servicePortBindings(service *kurtosis_core_rpc_api_bindings.ServiceInfo) []string {
	privatePorts := service.GetPrivatePorts()
	if len(privatePorts) == 0 {
		return []string{"<none>"}
	}

	portIDs := make([]string, 0, len(privatePorts))
	bindings := make(map[string]string, len(privatePorts))
	for id, privatePort := range privatePorts {
		portIDs = append(portIDs, id)
		bindings[id] = fmt.Sprintf(
			"%s: %d/%s",
			id,
			privatePort.GetNumber(),
			strings.ToLower(privatePort.GetTransportProtocol().String()),
		)
	}
	if publicIP := service.GetMaybePublicIpAddr(); publicIP != "" {
		for id, publicPort := range service.GetMaybePublicPorts() {
			privatePort, found := privatePorts[id]
			if !found {
				continue
			}
			protocol := privatePort.GetMaybeApplicationProtocol()
			if protocol != "" {
				protocol += "://"
			}
			bindings[id] += fmt.Sprintf(" -> %s%s:%d", protocol, publicIP, publicPort.GetNumber())
		}
	}
	sort.Strings(portIDs)
	result := make([]string, 0, len(portIDs))
	for _, id := range portIDs {
		result = append(result, bindings[id])
	}
	return result
}

// ServiceLogs streams all available log lines for every UUID to consume.
func (client *Client) ServiceLogs(
	ctx context.Context,
	enclaveName string,
	serviceUUIDs []string,
	consume ServiceLogConsumer,
) error {
	requested := make(map[string]bool, len(serviceUUIDs))
	for _, uuid := range serviceUUIDs {
		requested[uuid] = true
	}
	if len(requested) == 0 {
		return nil
	}

	followLogs := false
	returnAllLogs := true
	numLogLines := uint32(0)
	arguments := &kurtosis_engine_rpc_api_bindings.GetServiceLogsArgs{
		EnclaveIdentifier: enclaveName,
		ServiceUuidSet:    requested,
		FollowLogs:        &followLogs,
		ReturnAllLogs:     &returnAllLogs,
		NumLogLines:       &numLogLines,
	}
	getServiceLogs := client.getServiceLogs
	closeConnection := func() error { return nil }
	if getServiceLogs == nil {
		engineAddress := net.JoinHostPort(
			"127.0.0.1",
			strconv.Itoa(int(kurtosis_context.DefaultGrpcEngineServerPortNum)),
		)
		connection, err := grpc.NewClient(
			engineAddress,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxGRPCMessageSize)),
		)
		if err != nil {
			return fmt.Errorf("connect to Kurtosis log API: %w", err)
		}
		closeConnection = connection.Close
		logsClient := kurtosis_engine_rpc_api_bindings.NewEngineServiceClient(connection)
		getServiceLogs = func(
			ctx context.Context,
			arguments *kurtosis_engine_rpc_api_bindings.GetServiceLogsArgs,
		) (serviceLogsStream, error) {
			return logsClient.GetServiceLogs(ctx, arguments)
		}
	}

	stream, err := getServiceLogs(ctx, arguments)
	if err != nil {
		return errors.Join(
			fmt.Errorf("start Kurtosis service log stream: %w", err),
			closeConnection(),
		)
	}

	streamErr := receiveServiceLogs(stream, requested, consume)
	return errors.Join(
		streamErr,
		closeConnection(),
	)
}

func receiveServiceLogs(
	stream serviceLogsStream,
	requested map[string]bool,
	consume ServiceLogConsumer,
) error {
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("receive Kurtosis service logs: %w", err)
		}
		if response == nil {
			return errors.New("Kurtosis service log stream returned a nil response")
		}

		for uuid, logLines := range response.GetServiceLogsByServiceUuid() {
			if !requested[uuid] {
				continue
			}
			if consume != nil {
				consume(uuid, logLines.GetLine())
			}
		}
	}
}

func (client *Client) DestroyEnclave(ctx context.Context, name string) error {
	return client.engine.DestroyEnclave(ctx, name)
}

type serviceContext interface {
	GetServiceUUID() services.ServiceUUID
	GetPrivateIPAddress() string
	GetMaybePublicIPAddress() string
	GetPublicPorts() map[string]*services.PortSpec
	GetLabels() map[string]string
}

func newService(source serviceContext) Service {
	publicPorts := make(map[string]uint16, len(source.GetPublicPorts()))
	for id, port := range source.GetPublicPorts() {
		publicPorts[id] = port.GetNumber()
	}
	return Service{
		UUID:        string(source.GetServiceUUID()),
		PrivateIP:   source.GetPrivateIPAddress(),
		PublicIP:    source.GetMaybePublicIPAddress(),
		PublicPorts: publicPorts,
		Labels:      maps.Clone(source.GetLabels()),
	}
}

func consumeStarlarkCompletion(stream <-chan *kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine) error {
	var runErr error
	for line := range stream {
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
