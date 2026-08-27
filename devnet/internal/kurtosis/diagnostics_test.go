package kurtosis

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/kurtosis-tech/kurtosis/api/golang/core/kurtosis_core_rpc_api_bindings"
	"github.com/kurtosis-tech/kurtosis/api/golang/engine/kurtosis_engine_rpc_api_bindings"
	"github.com/stretchr/testify/require"
)

type fakeServiceLogsStream struct {
	responses   []*kurtosis_engine_rpc_api_bindings.GetServiceLogsResponse
	terminalErr error
	next        int
}

func (stream *fakeServiceLogsStream) Recv() (*kurtosis_engine_rpc_api_bindings.GetServiceLogsResponse, error) {
	if stream.next < len(stream.responses) {
		response := stream.responses[stream.next]
		stream.next++
		return response, nil
	}
	if stream.terminalErr != nil {
		err := stream.terminalErr
		stream.terminalErr = nil
		return nil, err
	}
	return nil, io.EOF
}

func TestServicePortBindings(t *testing.T) {
	service := &kurtosis_core_rpc_api_bindings.ServiceInfo{
		PrivatePorts: map[string]*kurtosis_core_rpc_api_bindings.Port{
			"rpc": {
				Number:                   8545,
				TransportProtocol:        kurtosis_core_rpc_api_bindings.Port_TCP,
				MaybeApplicationProtocol: "http",
			},
			"discovery": {
				Number:            30303,
				TransportProtocol: kurtosis_core_rpc_api_bindings.Port_UDP,
			},
		},
		MaybePublicIpAddr: "127.0.0.1",
		MaybePublicPorts: map[string]*kurtosis_core_rpc_api_bindings.Port{
			"rpc": {Number: 32000},
		},
	}
	require.Equal(t, []string{
		"discovery: 30303/udp",
		"rpc: 8545/tcp -> http://127.0.0.1:32000",
	}, servicePortBindings(service))
	require.Equal(t, []string{"<none>"}, servicePortBindings(new(kurtosis_core_rpc_api_bindings.ServiceInfo)))
}

func TestMergeServiceIdentities(t *testing.T) {
	identifiers := []*kurtosis_core_rpc_api_bindings.ServiceIdentifiers{
		{Name: "stale-name", ServiceUuid: "shared"},
		{Name: "same", ServiceUuid: "z"},
		{Name: "same", ServiceUuid: "a"},
	}
	current := map[string]*kurtosis_core_rpc_api_bindings.ServiceInfo{
		"shared": {
			Name:        "alpha",
			ServiceUuid: "shared",
			Container: &kurtosis_core_rpc_api_bindings.Container{
				Status: kurtosis_core_rpc_api_bindings.Container_RUNNING,
			},
		},
		"current-only": {
			Name:        "beta",
			ServiceUuid: "current-only",
			Container: &kurtosis_core_rpc_api_bindings.Container{
				Status: kurtosis_core_rpc_api_bindings.Container_RUNNING,
			},
		},
	}

	t.Run("complete current set", func(t *testing.T) {
		require.Equal(t, []ServiceIdentity{
			{Name: "alpha", UUID: "shared", Status: "RUNNING", Ports: []string{"<none>"}},
			{Name: "beta", UUID: "current-only", Status: "RUNNING", Ports: []string{"<none>"}},
			{Name: "same", UUID: "a", Status: "HISTORICAL", Ports: []string{"<unknown>"}},
			{Name: "same", UUID: "z", Status: "HISTORICAL", Ports: []string{"<unknown>"}},
		}, mergeServiceIdentities(identifiers, current, true))
	})
	t.Run("incomplete current set", func(t *testing.T) {
		require.Equal(t, []ServiceIdentity{
			{Name: "same", UUID: "a", Status: "UNKNOWN", Ports: []string{"<unknown>"}},
			{Name: "same", UUID: "z", Status: "UNKNOWN", Ports: []string{"<unknown>"}},
			{Name: "stale-name", UUID: "shared", Status: "UNKNOWN", Ports: []string{"<unknown>"}},
		}, mergeServiceIdentities(identifiers, nil, false))
	})
}

func TestInspectEnclaveStoppedAPI(t *testing.T) {
	info := &kurtosis_engine_rpc_api_bindings.EnclaveInfo{
		Name:               "test-enclave",
		EnclaveUuid:        "enclave-uuid",
		ApiContainerStatus: kurtosis_engine_rpc_api_bindings.EnclaveAPIContainerStatus_EnclaveAPIContainerStatus_STOPPED,
	}

	inspection, err := inspectEnclave(t.Context(), info)
	require.ErrorContains(t, err, "Kurtosis enclave has no creation time")
	require.ErrorContains(t, err, "Kurtosis enclave API container is not running: STOPPED")
	require.Equal(t, "test-enclave", inspection.Name)
	require.Equal(t, "enclave-uuid", inspection.UUID)
}

func TestServiceLogs(t *testing.T) {
	var request *kurtosis_engine_rpc_api_bindings.GetServiceLogsArgs
	getServiceLogs := func(
		_ context.Context,
		arguments *kurtosis_engine_rpc_api_bindings.GetServiceLogsArgs,
	) (serviceLogsStream, error) {
		request = arguments
		return &fakeServiceLogsStream{responses: []*kurtosis_engine_rpc_api_bindings.GetServiceLogsResponse{
			{
				ServiceLogsByServiceUuid: map[string]*kurtosis_engine_rpc_api_bindings.LogLine{
					"running-uuid": {Line: []string{"first", "second"}},
				},
			},
			{
				NotFoundServiceUuidSet: map[string]bool{"stopped-uuid": true},
			},
		}}, nil
	}

	captured := make(map[string][]string)
	notFound, err := serviceLogs(
		t.Context(),
		"test-enclave",
		[]string{"running-uuid", "stopped-uuid"},
		func(uuid string, lines []string) {
			captured[uuid] = append(captured[uuid], lines...)
		},
		getServiceLogs,
	)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"stopped-uuid": true}, notFound)
	require.Equal(t, map[string]bool{"running-uuid": true, "stopped-uuid": true}, request.GetServiceUuidSet())
	require.False(t, request.GetFollowLogs())
	require.True(t, request.GetReturnAllLogs())
	require.Equal(t, []string{"first", "second"}, captured["running-uuid"])
	require.NotContains(t, captured, "stopped-uuid")
}

func TestReceiveServiceLogsClearsNotFound(t *testing.T) {
	stream := &fakeServiceLogsStream{
		responses: []*kurtosis_engine_rpc_api_bindings.GetServiceLogsResponse{
			{NotFoundServiceUuidSet: map[string]bool{"service-uuid": true}},
			{ServiceLogsByServiceUuid: map[string]*kurtosis_engine_rpc_api_bindings.LogLine{
				"service-uuid": {},
			}},
		},
	}

	notFound, err := receiveServiceLogs(stream, map[string]bool{"service-uuid": true}, nil)
	require.NoError(t, err)
	require.Empty(t, notFound)
}

func TestReceiveServiceLogsKeepsNotFound(t *testing.T) {
	stream := &fakeServiceLogsStream{
		responses: []*kurtosis_engine_rpc_api_bindings.GetServiceLogsResponse{
			{ServiceLogsByServiceUuid: map[string]*kurtosis_engine_rpc_api_bindings.LogLine{
				"service-uuid": {},
			}},
			{NotFoundServiceUuidSet: map[string]bool{"service-uuid": true}},
		},
	}

	notFound, err := receiveServiceLogs(stream, map[string]bool{"service-uuid": true}, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"service-uuid": true}, notFound)
}

func TestServiceLogsStreamFailure(t *testing.T) {
	streamErr := errors.New("stream reset")
	getServiceLogs := func(
		context.Context,
		*kurtosis_engine_rpc_api_bindings.GetServiceLogsArgs,
	) (serviceLogsStream, error) {
		return &fakeServiceLogsStream{
			responses: []*kurtosis_engine_rpc_api_bindings.GetServiceLogsResponse{
				{NotFoundServiceUuidSet: map[string]bool{"missing-uuid": true}},
				{
					ServiceLogsByServiceUuid: map[string]*kurtosis_engine_rpc_api_bindings.LogLine{
						"service-uuid": {Line: []string{"partial"}},
					},
				},
			},
			terminalErr: streamErr,
		}, nil
	}

	var captured []string
	notFound, err := serviceLogs(
		t.Context(),
		"test-enclave",
		[]string{"service-uuid", "missing-uuid"},
		func(_ string, lines []string) { captured = append(captured, lines...) },
		getServiceLogs,
	)
	require.ErrorIs(t, err, streamErr)
	require.ErrorContains(t, err, "receive Kurtosis service logs: stream reset")
	require.Nil(t, notFound, "not-found state is provisional until the stream reaches EOF")
	require.Equal(t, []string{"partial"}, captured)
}

func TestServiceLogsEmptyOutput(t *testing.T) {
	for name, responses := range map[string][]*kurtosis_engine_rpc_api_bindings.GetServiceLogsResponse{
		"empty stream": nil,
		"empty response": {
			{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			getServiceLogs := func(
				context.Context,
				*kurtosis_engine_rpc_api_bindings.GetServiceLogsArgs,
			) (serviceLogsStream, error) {
				return &fakeServiceLogsStream{responses: responses}, nil
			}

			notFound, err := serviceLogs(
				t.Context(),
				"test-enclave",
				[]string{"service-uuid"},
				nil,
				getServiceLogs,
			)
			require.NoError(t, err)
			require.Empty(t, notFound)
		})
	}
}
