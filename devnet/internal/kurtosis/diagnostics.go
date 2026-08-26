package kurtosis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kurtosis-tech/kurtosis/api/golang/core/kurtosis_core_rpc_api_bindings"
	"github.com/kurtosis-tech/kurtosis/api/golang/engine/kurtosis_engine_rpc_api_bindings"
	"github.com/kurtosis-tech/kurtosis/api/golang/engine/lib/kurtosis_context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

const maxGRPCMessageSize = 100 * 1024 * 1024

type ServiceIdentity struct {
	Name   string   `json:"name"`
	UUID   string   `json:"uuid"`
	Status string   `json:"status"`
	Ports  []string `json:"ports"`
}

type FilesArtifactIdentity struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
}

type EnclaveInspection struct {
	Name           string                  `json:"name,omitempty"`
	UUID           string                  `json:"uuid,omitempty"`
	Status         string                  `json:"status,omitempty"`
	Mode           string                  `json:"mode,omitempty"`
	CreationTime   time.Time               `json:"creation_time,omitzero"`
	Services       []ServiceIdentity       `json:"services,omitempty"`
	FilesArtifacts []FilesArtifactIdentity `json:"files_artifacts,omitempty"`
}

type ServiceLogConsumer func(serviceUUID string, lines []string)

type serviceLogsStream interface {
	Recv() (*kurtosis_engine_rpc_api_bindings.GetServiceLogsResponse, error)
}

type getServiceLogsFunc func(
	ctx context.Context,
	arguments *kurtosis_engine_rpc_api_bindings.GetServiceLogsArgs,
) (serviceLogsStream, error)

// DiagnosticsClient owns the direct engine connection used to collect
// inspection data and retained logs independently of EnclaveClient.
type DiagnosticsClient struct {
	engine     kurtosis_engine_rpc_api_bindings.EngineServiceClient
	connection *grpc.ClientConn
}

// NewDiagnosticsClient opens the direct engine connection used by one
// diagnostics collection.
func NewDiagnosticsClient() (*DiagnosticsClient, error) {
	connection, err := newGRPCConnection(localEngineAddress())
	if err != nil {
		return nil, err
	}
	return &DiagnosticsClient{
		engine:     kurtosis_engine_rpc_api_bindings.NewEngineServiceClient(connection),
		connection: connection,
	}, nil
}

func (client *DiagnosticsClient) Close() error {
	return client.connection.Close()
}

// Inspect returns enclave metadata and the identifiers needed for diagnostics.
func (client *DiagnosticsClient) Inspect(
	ctx context.Context,
	enclaveName string,
) (EnclaveInspection, error) {
	response, err := client.engine.GetEnclavesByUuids(
		ctx,
		&kurtosis_engine_rpc_api_bindings.GetEnclavesByUuidsArgs{},
	)
	if err != nil {
		return EnclaveInspection{}, fmt.Errorf("list running Kurtosis enclaves: %w", err)
	}
	for _, info := range response.GetEnclaveInfo() {
		if info.GetName() == enclaveName {
			return inspectEnclave(ctx, info)
		}
	}
	return EnclaveInspection{}, fmt.Errorf("no running Kurtosis enclave named %q", enclaveName)
}

func inspectEnclave(
	ctx context.Context,
	info *kurtosis_engine_rpc_api_bindings.EnclaveInfo,
) (EnclaveInspection, error) {
	inspection := EnclaveInspection{
		Name:   info.GetName(),
		UUID:   info.GetEnclaveUuid(),
		Status: strings.TrimPrefix(info.GetContainersStatus().String(), "EnclaveContainersStatus_"),
		Mode:   info.GetMode().String(),
	}
	var inspectionErrors []error
	if created := info.GetCreationTime(); created != nil {
		inspection.CreationTime = created.AsTime()
	} else {
		inspectionErrors = append(inspectionErrors, errors.New("Kurtosis enclave has no creation time"))
	}
	apiStatus := info.GetApiContainerStatus()
	if apiStatus != kurtosis_engine_rpc_api_bindings.EnclaveAPIContainerStatus_EnclaveAPIContainerStatus_RUNNING {
		status := strings.TrimPrefix(apiStatus.String(), "EnclaveAPIContainerStatus_")
		inspectionErrors = append(
			inspectionErrors,
			fmt.Errorf("Kurtosis enclave API container is not running: %s", status),
		)
		return inspection, errors.Join(inspectionErrors...)
	}

	services, artifacts, err := inspectEnclaveContents(ctx, info)
	inspection.Services = services
	inspection.FilesArtifacts = artifacts
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
	connection, err := newGRPCConnection(address)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to Kurtosis API container: %w", err)
	}
	defer func() { _ = connection.Close() }()
	api := kurtosis_core_rpc_api_bindings.NewApiContainerServiceClient(connection)
	historical, historicalErr := api.GetExistingAndHistoricalServiceIdentifiers(ctx, &emptypb.Empty{})
	current, currentErr := api.GetServices(ctx, &kurtosis_core_rpc_api_bindings.GetServicesArgs{
		ServiceIdentifiers: map[string]bool{},
	})
	artifacts, artifactsErr := api.ListFilesArtifactNamesAndUuids(ctx, &emptypb.Empty{})
	if historicalErr != nil {
		historicalErr = fmt.Errorf("get historical Kurtosis services: %w", historicalErr)
	}
	if currentErr != nil {
		currentErr = fmt.Errorf("get current Kurtosis services: %w", currentErr)
	}
	if artifactsErr != nil {
		artifactsErr = fmt.Errorf("get Kurtosis files artifacts: %w", artifactsErr)
	}

	historicalStatus := "UNKNOWN"
	if currentErr == nil {
		historicalStatus = "HISTORICAL"
	}
	servicesByUUID := make(map[string]ServiceIdentity)
	for _, identifier := range historical.GetAllIdentifiers() {
		servicesByUUID[identifier.GetServiceUuid()] = ServiceIdentity{
			Name:   identifier.GetName(),
			UUID:   identifier.GetServiceUuid(),
			Status: historicalStatus,
			Ports:  []string{"<unknown>"},
		}
	}
	for _, service := range current.GetServiceInfo() {
		servicesByUUID[service.GetServiceUuid()] = ServiceIdentity{
			Name:   service.GetName(),
			UUID:   service.GetServiceUuid(),
			Status: service.GetContainer().GetStatus().String(),
			Ports:  servicePortBindings(service),
		}
	}
	services := slices.Collect(maps.Values(servicesByUUID))
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

	return services, files, errors.Join(historicalErr, currentErr, artifactsErr)
}

func servicePortBindings(service *kurtosis_core_rpc_api_bindings.ServiceInfo) []string {
	privatePorts := service.GetPrivatePorts()
	if len(privatePorts) == 0 {
		return []string{"<none>"}
	}

	portIDs := slices.Sorted(maps.Keys(privatePorts))
	result := make([]string, 0, len(portIDs))
	publicIP := service.GetMaybePublicIpAddr()
	publicPorts := service.GetMaybePublicPorts()
	for _, id := range portIDs {
		privatePort := privatePorts[id]
		binding := fmt.Sprintf(
			"%s: %d/%s",
			id,
			privatePort.GetNumber(),
			strings.ToLower(privatePort.GetTransportProtocol().String()),
		)
		if publicPort, found := publicPorts[id]; found && publicIP != "" {
			protocol := privatePort.GetMaybeApplicationProtocol()
			if protocol != "" {
				protocol += "://"
			}
			binding += fmt.Sprintf(" -> %s%s:%d", protocol, publicIP, publicPort.GetNumber())
		}
		result = append(result, binding)
	}
	return result
}

// ServiceLogs streams all available log lines for every UUID to consume and
// returns the requested UUIDs the engine could not find.
func (client *DiagnosticsClient) ServiceLogs(
	ctx context.Context,
	enclaveName string,
	serviceUUIDs []string,
	consume ServiceLogConsumer,
) (map[string]bool, error) {
	return serviceLogs(ctx, enclaveName, serviceUUIDs, consume, engineServiceLogs(client.engine))
}

func engineServiceLogs(client kurtosis_engine_rpc_api_bindings.EngineServiceClient) getServiceLogsFunc {
	return func(
		ctx context.Context,
		arguments *kurtosis_engine_rpc_api_bindings.GetServiceLogsArgs,
	) (serviceLogsStream, error) {
		return client.GetServiceLogs(ctx, arguments)
	}
}

func serviceLogs(
	ctx context.Context,
	enclaveName string,
	serviceUUIDs []string,
	consume ServiceLogConsumer,
	getServiceLogs getServiceLogsFunc,
) (map[string]bool, error) {
	requested := make(map[string]bool, len(serviceUUIDs))
	for _, uuid := range serviceUUIDs {
		requested[uuid] = true
	}
	if len(requested) == 0 {
		return nil, nil
	}

	followLogs := false
	returnAllLogs := true
	numLogLines := uint32(0)
	stream, err := getServiceLogs(ctx, &kurtosis_engine_rpc_api_bindings.GetServiceLogsArgs{
		EnclaveIdentifier: enclaveName,
		ServiceUuidSet:    requested,
		FollowLogs:        &followLogs,
		ReturnAllLogs:     &returnAllLogs,
		NumLogLines:       &numLogLines,
	})
	if err != nil {
		return nil, fmt.Errorf("start Kurtosis service log stream: %w", err)
	}
	return receiveServiceLogs(stream, requested, consume)
}

func receiveServiceLogs(
	stream serviceLogsStream,
	requested map[string]bool,
	consume ServiceLogConsumer,
) (map[string]bool, error) {
	notFound := make(map[string]bool)
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return notFound, nil
		}
		if err != nil {
			return nil, fmt.Errorf("receive Kurtosis service logs: %w", err)
		}

		for uuid := range response.GetNotFoundServiceUuidSet() {
			if requested[uuid] {
				notFound[uuid] = true
			}
		}
		for uuid, logLines := range response.GetServiceLogsByServiceUuid() {
			if !requested[uuid] {
				continue
			}
			delete(notFound, uuid)
			if consume != nil {
				consume(uuid, logLines.GetLine())
			}
		}
	}
}

func localEngineAddress() string {
	return net.JoinHostPort(
		"127.0.0.1",
		strconv.Itoa(int(kurtosis_context.DefaultGrpcEngineServerPortNum)),
	)
}

func newGRPCConnection(address string) (*grpc.ClientConn, error) {
	return grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxGRPCMessageSize)),
	)
}
