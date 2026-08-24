package devnet

import (
	"context"
	"errors"
	"strings"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestResolveExecutionImage(t *testing.T) {
	imageID := "sha256:" + strings.Repeat("ab", 32)
	listContainers := func(
		_ context.Context,
		options dockerclient.ContainerListOptions,
	) (dockerclient.ContainerListResult, error) {
		require.False(t, options.All)
		require.Equal(t, dockerclient.Filters{
			"label": {kurtosisServiceUUIDDockerLabel + "=primary-execution-service": true},
		}, options.Filters)
		return dockerclient.ContainerListResult{Items: []containertypes.Summary{{
			ImageID: imageID,
		}}}, nil
	}

	resolved, err := resolveExecutionImage(t.Context(), "primary-execution-service", listContainers)
	require.NoError(t, err)
	require.Equal(t, imageID, resolved)
}

func TestResolveExecutionImageErrors(t *testing.T) {
	for name, testCase := range map[string]struct {
		containers []containertypes.Summary
		clientErr  error
		wantErr    string
	}{
		"no matching container": {
			wantErr: "expected one running Docker container for service \"primary-execution-service\", found 0",
		},
		"multiple matching containers": {
			containers: []containertypes.Summary{{ID: "first"}, {ID: "second"}},
			wantErr:    "expected one running Docker container for service \"primary-execution-service\", found 2",
		},
		"list failure": {
			clientErr: errors.New("list failed"),
			wantErr:   "find primary execution container: list failed",
		},
		"malformed image ID": {
			containers: []containertypes.Summary{{
				ID:      strings.Repeat("cd", 32),
				ImageID: "registry.example/go-qrl:mutable",
			}},
			wantErr: "invalid Docker image ID",
		},
	} {
		t.Run(name, func(t *testing.T) {
			listContainers := func(
				context.Context,
				dockerclient.ContainerListOptions,
			) (dockerclient.ContainerListResult, error) {
				return dockerclient.ContainerListResult{Items: testCase.containers}, testCase.clientErr
			}

			_, err := resolveExecutionImage(t.Context(), "primary-execution-service", listContainers)
			require.ErrorContains(t, err, testCase.wantErr)
		})
	}
}

func TestPrimaryExecutionServiceID(t *testing.T) {
	environment := executionImageTestEnvironment()
	environment.Backend = BackendKubernetes
	_, err := primaryExecutionServiceID(environment)
	require.ErrorContains(t, err, "is not Docker")

	environment.Backend = BackendDocker
	environment.Participants[0].Execution.ID = ""
	_, err = primaryExecutionServiceID(environment)
	require.ErrorContains(t, err, "primary execution service has no ID")
}

func executionImageTestEnvironment() Environment {
	return Environment{
		Backend: BackendDocker,
		Participants: []Participant{{
			Index:     1,
			Execution: ExecutionService{ServiceInfo: ServiceInfo{ID: "primary-execution-service"}},
		}},
	}
}
