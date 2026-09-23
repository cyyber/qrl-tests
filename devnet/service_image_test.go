package devnet

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/cyyber/qrl-tests/internal/containerfiles"
	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestResolveContainerImage(t *testing.T) {
	imageID := "sha256:" + strings.Repeat("ab", 32)
	client := &fakeDocker{containers: []containertypes.Summary{{ImageID: imageID}}}

	resolved, err := resolveContainerImage(t.Context(), "primary-execution-service", "execution", client)
	require.NoError(t, err)
	require.Equal(t, imageID, resolved)
	require.False(t, client.listed.All)
	require.Equal(t, dockerclient.Filters{
		"label": {kurtosisServiceUUIDDockerLabel + "=primary-execution-service": true},
	}, client.listed.Filters)
}

func TestResolveContainerImageErrors(t *testing.T) {
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
			client := &fakeDocker{containers: testCase.containers, listErr: testCase.clientErr}

			_, err := resolveContainerImage(t.Context(), "primary-execution-service", "execution", client)
			require.ErrorContains(t, err, testCase.wantErr)
		})
	}
}

func TestPrimaryServiceIDs(t *testing.T) {
	environment := Environment{
		Backend: BackendDocker,
		Participants: []Participant{{
			Index:     1,
			Execution: ExecutionService{ServiceInfo: ServiceInfo{ID: "primary-execution-service"}},
			Validator: ValidatorService{ServiceInfo: ServiceInfo{ID: "primary-validator-service"}},
		}},
	}
	serviceID, err := primaryServiceID(environment, "execution", func(participant Participant) string {
		return participant.Execution.ID
	})
	require.NoError(t, err)
	require.Equal(t, "primary-execution-service", serviceID)

	serviceID, err = primaryServiceID(environment, "validator", func(participant Participant) string {
		return participant.Validator.ID
	})
	require.NoError(t, err)
	require.Equal(t, "primary-validator-service", serviceID)

	environment.Backend = BackendKubernetes
	_, err = primaryServiceID(environment, "validator", func(participant Participant) string {
		return participant.Validator.ID
	})
	require.ErrorContains(t, err, "is not Docker")

	environment.Backend = BackendDocker
	environment.Participants[0].Validator.ID = ""
	_, err = primaryServiceID(environment, "validator", func(participant Participant) string {
		return participant.Validator.ID
	})
	require.ErrorContains(t, err, "primary validator service has no ID")
}

func TestReadServiceFile(t *testing.T) {
	client := &fakeDocker{
		containers: []containertypes.Summary{{ID: "consensus-container"}},
		entries:    map[string]string{"config.yaml": "PRESET_BASE: minimal\n"},
	}

	body, err := ReadServiceFile(t.Context(), client, "consensus-service", "/network-configs/config.yaml")
	require.NoError(t, err)
	require.Equal(t, "PRESET_BASE: minimal\n", string(body))
	require.Equal(t, dockerclient.Filters{
		"label": {kurtosisServiceUUIDDockerLabel + "=consensus-service": true},
	}, client.listed.Filters)
	require.Equal(t, "consensus-container", client.copiedFrom)
	require.Equal(t, "/network-configs/config.yaml", client.copiedPath)
}

func TestReadServiceFileErrors(t *testing.T) {
	for name, testCase := range map[string]struct {
		client  *fakeDocker
		wantErr string
	}{
		"no running container": {
			client:  &fakeDocker{},
			wantErr: `find container: expected one running Docker container for service "consensus-service", found 0`,
		},
		"list failure": {
			client:  &fakeDocker{listErr: errors.New("list failed")},
			wantErr: "find container: list failed",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ReadServiceFile(t.Context(), testCase.client, "consensus-service", "/network-configs/config.yaml")
			require.EqualError(t, err, testCase.wantErr)
		})
	}
}

// fakeDocker serves ContainerList and CopyFromContainer from memory.
type fakeDocker struct {
	containers []containertypes.Summary
	listErr    error
	// entries are the archive entries CopyFromContainer returns, by name.
	entries map[string]string

	listed     dockerclient.ContainerListOptions
	copiedFrom string
	copiedPath string
}

func (client *fakeDocker) ContainerList(_ context.Context, options dockerclient.ContainerListOptions) (dockerclient.ContainerListResult, error) {
	client.listed = options
	return dockerclient.ContainerListResult{Items: client.containers}, client.listErr
}

func (client *fakeDocker) CopyFromContainer(_ context.Context, containerID string, options dockerclient.CopyFromContainerOptions) (dockerclient.CopyFromContainerResult, error) {
	client.copiedFrom = containerID
	client.copiedPath = options.SourcePath
	var files []containerfiles.File
	for name, body := range client.entries {
		files = append(files, containerfiles.File{Name: name, Body: []byte(body)})
	}
	archive, err := containerfiles.Archive(files)
	if err != nil {
		return dockerclient.CopyFromContainerResult{}, err
	}
	return dockerclient.CopyFromContainerResult{Content: io.NopCloser(bytes.NewReader(archive))}, nil
}
