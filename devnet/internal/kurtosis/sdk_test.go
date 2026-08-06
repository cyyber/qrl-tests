package kurtosis

import (
	"testing"

	"github.com/kurtosis-tech/kurtosis/api/golang/core/kurtosis_core_rpc_api_bindings"
	"github.com/kurtosis-tech/kurtosis/api/golang/core/lib/services"
	"github.com/stretchr/testify/require"
)

type fakeServiceContext struct {
	labels map[string]string
	ports  map[string]*services.PortSpec
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

func TestConsumeStarlarkCompletionReturnsStructuredError(t *testing.T) {
	stream := make(chan *kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine, 2)
	stream <- &kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine{
		RunResponseLine: &kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine_Error{
			Error: &kurtosis_core_rpc_api_bindings.StarlarkError{
				Error: &kurtosis_core_rpc_api_bindings.StarlarkError_ExecutionError{
					ExecutionError: &kurtosis_core_rpc_api_bindings.StarlarkExecutionError{
						ErrorMessage: "vc_extra_params must be a list",
					},
				},
			},
		},
	}
	stream <- &kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine{
		RunResponseLine: &kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine_RunFinishedEvent{
			RunFinishedEvent: &kurtosis_core_rpc_api_bindings.StarlarkRunFinishedEvent{},
		},
	}
	close(stream)

	err := consumeStarlarkCompletion(stream)
	require.EqualError(t, err, "Kurtosis Starlark execution failed: vc_extra_params must be a list")
}
