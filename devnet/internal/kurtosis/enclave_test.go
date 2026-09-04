package kurtosis

import (
	"errors"
	"testing"

	"github.com/kurtosis-tech/kurtosis/api/golang/core/kurtosis_core_rpc_api_bindings"
	"github.com/kurtosis-tech/kurtosis/api/golang/core/lib/services"
	"github.com/stretchr/testify/require"
)

type fakeServiceContext struct {
	labels       map[string]string
	ports        map[string]*services.PortSpec
	privatePorts map[string]*services.PortSpec
}

func (*fakeServiceContext) GetServiceUUID() services.ServiceUUID { return "svc-uuid" }

func (*fakeServiceContext) GetPrivateIPAddress() string { return "10.0.0.7" }

func (*fakeServiceContext) GetMaybePublicIPAddress() string { return "127.0.0.1" }

func (fake *fakeServiceContext) GetPublicPorts() map[string]*services.PortSpec { return fake.ports }

func (fake *fakeServiceContext) GetPrivatePorts() map[string]*services.PortSpec {
	return fake.privatePorts
}

func (fake *fakeServiceContext) GetLabels() map[string]string { return fake.labels }

func TestNewServiceCopiesContext(t *testing.T) {
	labels := map[string]string{"qrl-package.client-type": "execution"}
	source := &fakeServiceContext{
		labels: labels,
		ports: map[string]*services.PortSpec{
			"rpc": services.NewPortSpec(3200, services.TransportProtocol_TCP, "http"),
		},
		privatePorts: map[string]*services.PortSpec{
			"rpc": services.NewPortSpec(8545, services.TransportProtocol_TCP, "http"),
		},
	}

	converted := newService(source)
	require.Equal(t, Service{
		UUID:         "svc-uuid",
		PrivateIP:    "10.0.0.7",
		PublicIP:     "127.0.0.1",
		PublicPorts:  map[string]uint16{"rpc": 3200},
		PrivatePorts: map[string]uint16{"rpc": 8545},
		Labels:       map[string]string{"qrl-package.client-type": "execution"},
	}, converted)

	// The conversion must copy: SDK-owned maps cannot leak into the result.
	labels["qrl-package.client-type"] = "mutated"
	require.Equal(t, "execution", converted.Labels["qrl-package.client-type"])
}

func TestServiceEndpoints(t *testing.T) {
	service := Service{
		PrivateIP:    "10.0.0.7",
		PublicIP:     "127.0.0.1",
		PublicPorts:  map[string]uint16{"rpc": 3200},
		PrivatePorts: map[string]uint16{"rpc": 8545},
	}

	public, err := service.PublicEndpoint("rpc", "http")
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:3200", public)

	private, err := service.PrivateEndpoint("rpc", "http")
	require.NoError(t, err)
	require.Equal(t, "http://10.0.0.7:8545", private)

	_, err = service.PrivateEndpoint("ws", "ws")
	require.ErrorContains(t, err, `no private "ws" port`)

	_, err = Service{PrivatePorts: map[string]uint16{"rpc": 1}}.PrivateEndpoint("rpc", "http")
	require.ErrorContains(t, err, "no private IP address")
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

func TestTransientKurtosisError(t *testing.T) {
	require.False(t, transientKurtosisError(nil))
	require.False(t, transientKurtosisError(errors.New("no qrl-package participants found")))
	require.True(t, transientKurtosisError(errors.New("rpc error: code = Unavailable desc = error reading from server: EOF")))
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
