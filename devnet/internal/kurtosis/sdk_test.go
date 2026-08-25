package kurtosis

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/kurtosis-tech/kurtosis/api/golang/core/kurtosis_core_rpc_api_bindings"
	"github.com/kurtosis-tech/kurtosis/api/golang/core/lib/services"
	"github.com/kurtosis-tech/kurtosis/api/golang/engine/kurtosis_engine_rpc_api_bindings"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type engineInfoServer struct {
	kurtosis_engine_rpc_api_bindings.UnimplementedEngineServiceServer
	started chan struct{}
}

func (server *engineInfoServer) GetEngineInfo(
	ctx context.Context,
	_ *emptypb.Empty,
) (*kurtosis_engine_rpc_api_bindings.GetEngineInfoResponse, error) {
	if server.started != nil {
		close(server.started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &kurtosis_engine_rpc_api_bindings.GetEngineInfoResponse{EngineVersion: "1.20.0"}, nil
}

func serveEngine(t *testing.T, engine kurtosis_engine_rpc_api_bindings.EngineServiceServer) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	kurtosis_engine_rpc_api_bindings.RegisterEngineServiceServer(server, engine)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}

type fakeServiceContext struct {
	labels map[string]string
	ports  map[string]*services.PortSpec
}

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

func (*fakeServiceContext) GetServiceUUID() services.ServiceUUID { return "svc-uuid" }

func (*fakeServiceContext) GetPrivateIPAddress() string { return "10.0.0.7" }

func (*fakeServiceContext) GetMaybePublicIPAddress() string { return "127.0.0.1" }

func (fake *fakeServiceContext) GetPublicPorts() map[string]*services.PortSpec { return fake.ports }

func (fake *fakeServiceContext) GetLabels() map[string]string { return fake.labels }

func TestNewDiagnosticsClient(t *testing.T) {
	client, err := newDiagnosticsClient(t.Context(), serveEngine(t, new(engineInfoServer)))
	require.NoError(t, err)
	require.NoError(t, client.Close())
}

func TestNewDiagnosticsClientHonorsCancellation(t *testing.T) {
	started := make(chan struct{})
	address := serveEngine(t, &engineInfoServer{started: started})
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := newDiagnosticsClient(ctx, address)
		result <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("engine validation did not start")
	}
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("client creation did not stop after cancellation")
	}
}

func TestNewServiceCopiesContext(t *testing.T) {
	labels := map[string]string{"qrl-package.client-type": "execution"}
	source := &fakeServiceContext{
		labels: labels,
		ports: map[string]*services.PortSpec{
			"rpc": services.NewPortSpec(3200, services.TransportProtocol_TCP, "http"),
		},
	}

	converted := newService(source)
	require.Equal(t, Service{
		UUID:        "svc-uuid",
		PrivateIP:   "10.0.0.7",
		PublicIP:    "127.0.0.1",
		PublicPorts: map[string]uint16{"rpc": 3200},
		Labels:      map[string]string{"qrl-package.client-type": "execution"},
	}, converted)

	// The conversion must copy: SDK-owned maps cannot leak into the result.
	labels["qrl-package.client-type"] = "mutated"
	require.Equal(t, "execution", converted.Labels["qrl-package.client-type"])
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

func TestInspectEnclaveRejectsStoppedAPIContainer(t *testing.T) {
	info := &kurtosis_engine_rpc_api_bindings.EnclaveInfo{
		Name:               "test-enclave",
		EnclaveUuid:        "enclave-uuid",
		ApiContainerStatus: kurtosis_engine_rpc_api_bindings.EnclaveAPIContainerStatus_EnclaveAPIContainerStatus_STOPPED,
		CreationTime:       timestamppb.Now(),
	}

	inspection, err := inspectEnclave(t.Context(), info)
	require.ErrorContains(t, err, "Kurtosis enclave API container is not running: STOPPED")
	require.Equal(t, "test-enclave", inspection.Name)
	require.Equal(t, "enclave-uuid", inspection.UUID)
}

func TestServiceLogs(t *testing.T) {
	var request *kurtosis_engine_rpc_api_bindings.GetServiceLogsArgs
	client := &Client{
		getServiceLogs: func(
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
		},
	}

	captured := make(map[string][]string)
	notFound, err := client.ServiceLogs(
		t.Context(),
		"test-enclave",
		[]string{"running-uuid", "stopped-uuid"},
		func(uuid string, lines []string) {
			captured[uuid] = append(captured[uuid], lines...)
		},
	)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"stopped-uuid": true}, notFound)
	require.Equal(t, map[string]bool{"running-uuid": true, "stopped-uuid": true}, request.GetServiceUuidSet())
	require.False(t, request.GetFollowLogs())
	require.True(t, request.GetReturnAllLogs())
	require.Equal(t, []string{"first", "second"}, captured["running-uuid"])
	require.NotContains(t, captured, "stopped-uuid")
}

func TestReceiveServiceLogsClearsEarlierNotFound(t *testing.T) {
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

func TestReceiveServiceLogsKeepsLaterNotFound(t *testing.T) {
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
	client := &Client{
		getServiceLogs: func(
			context.Context,
			*kurtosis_engine_rpc_api_bindings.GetServiceLogsArgs,
		) (serviceLogsStream, error) {
			return &fakeServiceLogsStream{
				responses: []*kurtosis_engine_rpc_api_bindings.GetServiceLogsResponse{
					{
						ServiceLogsByServiceUuid: map[string]*kurtosis_engine_rpc_api_bindings.LogLine{
							"service-uuid": {Line: []string{"partial"}},
						},
					},
				},
				terminalErr: errors.New("stream reset"),
			}, nil
		},
	}

	var captured []string
	notFound, err := client.ServiceLogs(
		t.Context(),
		"test-enclave",
		[]string{"service-uuid"},
		func(_ string, lines []string) { captured = append(captured, lines...) },
	)
	require.ErrorContains(t, err, "receive Kurtosis service logs: stream reset")
	require.Empty(t, notFound)
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
			client := &Client{
				getServiceLogs: func(
					context.Context,
					*kurtosis_engine_rpc_api_bindings.GetServiceLogsArgs,
				) (serviceLogsStream, error) {
					return &fakeServiceLogsStream{responses: responses}, nil
				},
			}

			notFound, err := client.ServiceLogs(t.Context(), "test-enclave", []string{"service-uuid"}, nil)
			require.NoError(t, err)
			require.Empty(t, notFound)
		})
	}
}

func TestServiceLogsRejectsNilResponse(t *testing.T) {
	client := &Client{
		getServiceLogs: func(
			context.Context,
			*kurtosis_engine_rpc_api_bindings.GetServiceLogsArgs,
		) (serviceLogsStream, error) {
			return &fakeServiceLogsStream{
				responses: []*kurtosis_engine_rpc_api_bindings.GetServiceLogsResponse{nil},
			}, nil
		},
	}
	_, err := client.ServiceLogs(t.Context(), "test-enclave", []string{"service-uuid"}, nil)
	require.ErrorContains(t, err, "nil response")
}

func errorLine(message string) *kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine {
	return &kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine{
		RunResponseLine: &kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine_Error{
			Error: &kurtosis_core_rpc_api_bindings.StarlarkError{
				Error: &kurtosis_core_rpc_api_bindings.StarlarkError_ExecutionError{
					ExecutionError: &kurtosis_core_rpc_api_bindings.StarlarkExecutionError{ErrorMessage: message},
				},
			},
		},
	}
}

func finishLine(successful bool) *kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine {
	return &kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine{
		RunResponseLine: &kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine_RunFinishedEvent{
			RunFinishedEvent: &kurtosis_core_rpc_api_bindings.StarlarkRunFinishedEvent{IsRunSuccessful: successful},
		},
	}
}

func TestConsumeStarlarkCompletion(t *testing.T) {
	for name, test := range map[string]struct {
		lines   []*kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine
		wantErr string
	}{
		"successful run": {
			lines: []*kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine{finishLine(true)},
		},
		"structured failure": {
			lines: []*kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine{
				errorLine("vc_extra_params must be a list"),
				finishLine(false),
			},
			wantErr: "Kurtosis Starlark execution failed: vc_extra_params must be a list",
		},
		"failure without a structured error": {
			lines:   []*kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine{finishLine(false)},
			wantErr: "Kurtosis Starlark package run failed without a structured error",
		},
		"truncated stream after an error": {
			lines:   []*kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine{errorLine("boom")},
			wantErr: "Kurtosis Starlark execution failed: boom",
		},
		"truncated stream without an error": {
			wantErr: "Kurtosis Starlark response stream closed without a terminal event",
		},
		"accumulates every error": {
			lines: []*kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine{
				errorLine("first"),
				errorLine("second"),
				finishLine(false),
			},
			wantErr: "Kurtosis Starlark execution failed: first\nKurtosis Starlark execution failed: second",
		},
	} {
		t.Run(name, func(t *testing.T) {
			stream := make(chan *kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine, len(test.lines))
			for _, line := range test.lines {
				stream <- line
			}
			close(stream)

			err := consumeStarlarkCompletion(stream)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, test.wantErr)
		})
	}
}

func TestStarlarkErrorKinds(t *testing.T) {
	for name, test := range map[string]struct {
		input *kurtosis_core_rpc_api_bindings.StarlarkError
		want  string
	}{
		"interpretation": {
			input: &kurtosis_core_rpc_api_bindings.StarlarkError{
				Error: &kurtosis_core_rpc_api_bindings.StarlarkError_InterpretationError{
					InterpretationError: &kurtosis_core_rpc_api_bindings.StarlarkInterpretationError{ErrorMessage: "bad syntax"},
				},
			},
			want: "Kurtosis Starlark interpretation failed: bad syntax",
		},
		"validation": {
			input: &kurtosis_core_rpc_api_bindings.StarlarkError{
				Error: &kurtosis_core_rpc_api_bindings.StarlarkError_ValidationError{
					ValidationError: &kurtosis_core_rpc_api_bindings.StarlarkValidationError{ErrorMessage: "bad plan"},
				},
			},
			want: "Kurtosis Starlark validation failed: bad plan",
		},
		"execution": {
			input: errorLine("bad run").GetError(),
			want:  "Kurtosis Starlark execution failed: bad run",
		},
		"unknown": {
			input: &kurtosis_core_rpc_api_bindings.StarlarkError{},
			want:  "Kurtosis Starlark package run failed with an unknown structured error",
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.EqualError(t, starlarkError(test.input), test.want)
		})
	}
}
