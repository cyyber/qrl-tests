package kurtosis

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/kurtosis-tech/kurtosis/api/golang/core/kurtosis_core_rpc_api_bindings"
	"github.com/kurtosis-tech/kurtosis/api/golang/core/lib/services"
	"github.com/kurtosis-tech/kurtosis/api/golang/engine/kurtosis_engine_rpc_api_bindings"
	"github.com/stretchr/testify/require"
)

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
	err := client.ServiceLogs(
		t.Context(),
		"test-enclave",
		[]string{"running-uuid", "stopped-uuid"},
		func(uuid string, lines []string) {
			captured[uuid] = append(captured[uuid], lines...)
		},
	)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"running-uuid": true, "stopped-uuid": true}, request.GetServiceUuidSet())
	require.False(t, request.GetFollowLogs())
	require.True(t, request.GetReturnAllLogs())
	require.Equal(t, []string{"first", "second"}, captured["running-uuid"])
	require.NotContains(t, captured, "stopped-uuid")
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
	err := client.ServiceLogs(
		t.Context(),
		"test-enclave",
		[]string{"service-uuid"},
		func(_ string, lines []string) { captured = append(captured, lines...) },
	)
	require.ErrorContains(t, err, "receive Kurtosis service logs: stream reset")
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

			err := client.ServiceLogs(t.Context(), "test-enclave", []string{"service-uuid"}, nil)
			require.NoError(t, err)
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
	require.ErrorContains(
		t,
		client.ServiceLogs(t.Context(), "test-enclave", []string{"service-uuid"}, nil),
		"nil response",
	)
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
