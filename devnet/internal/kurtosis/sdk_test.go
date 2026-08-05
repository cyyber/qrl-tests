package kurtosis

import (
	"testing"

	"github.com/kurtosis-tech/kurtosis/api/golang/core/kurtosis_core_rpc_api_bindings"
	"github.com/stretchr/testify/require"
)

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
