package devnet

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/cyyber/qrl-tests/internal/containerfiles"
	"github.com/cyyber/qrl-tests/internal/dockerapi"
	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
)

const kurtosisServiceUUIDDockerLabel = "com.kurtosistech.guid"

// ResolveExecutionImage returns the immutable Docker image ID used by the
// primary execution service's running container.
func ResolveExecutionImage(ctx context.Context, environment Environment) (string, error) {
	return resolvePrimaryServiceImage(ctx, environment, "execution", func(participant Participant) string {
		return participant.Execution.ID
	})
}

// ResolveValidatorImage returns the immutable Docker image ID used by the
// primary validator service's running container.
func ResolveValidatorImage(ctx context.Context, environment Environment) (string, error) {
	return resolvePrimaryServiceImage(ctx, environment, "validator", func(participant Participant) string {
		return participant.Validator.ID
	})
}

func resolvePrimaryServiceImage(
	ctx context.Context,
	environment Environment,
	role string,
	serviceID func(Participant) string,
) (string, error) {
	id, err := primaryServiceID(environment, role, serviceID)
	if err != nil {
		return "", err
	}
	client, err := dockerapi.New()
	if err != nil {
		return "", fmt.Errorf("create Docker client: %w", err)
	}
	defer func() { _ = client.Close() }()
	return resolveContainerImage(ctx, id, role, client)
}

func primaryServiceID(environment Environment, role string, serviceID func(Participant) string) (string, error) {
	if environment.Backend != BackendDocker {
		return "", fmt.Errorf("backend %q is not Docker", environment.Backend)
	}
	primary, err := environment.Primary()
	if err != nil {
		return "", fmt.Errorf("select primary participant: %w", err)
	}
	id := strings.TrimSpace(serviceID(primary))
	if id == "" {
		return "", fmt.Errorf("primary %s service has no ID", role)
	}
	return id, nil
}

func resolveContainerImage(ctx context.Context, serviceID, role string, client containerLister) (string, error) {
	container, err := serviceContainer(ctx, client, serviceID)
	if err != nil {
		return "", fmt.Errorf("find primary %s container: %w", role, err)
	}

	imageID := strings.TrimSpace(container.ImageID)
	if !validSHA256ID(imageID) {
		return "", fmt.Errorf("invalid Docker image ID %q", imageID)
	}
	return imageID, nil
}

func validSHA256ID(value string) bool {
	encoded, found := strings.CutPrefix(value, "sha256:")
	if !found || len(encoded) != 64 {
		return false
	}
	_, err := hex.DecodeString(encoded)
	return err == nil
}

// containerLister is the part of the Docker client serviceContainer uses.
type containerLister interface {
	ContainerList(context.Context, dockerclient.ContainerListOptions) (dockerclient.ContainerListResult, error)
}

// serviceContainer returns the running Docker container of a Kurtosis service.
func serviceContainer(ctx context.Context, client containerLister, serviceID string) (containertypes.Summary, error) {
	containers, err := client.ContainerList(ctx, dockerclient.ContainerListOptions{
		Filters: make(dockerclient.Filters).Add("label", kurtosisServiceUUIDDockerLabel+"="+serviceID),
	})
	if err != nil {
		return containertypes.Summary{}, err
	}
	if len(containers.Items) != 1 {
		return containertypes.Summary{}, fmt.Errorf(
			"expected one running Docker container for service %q, found %d",
			serviceID,
			len(containers.Items),
		)
	}
	return containers.Items[0], nil
}

// ServiceFileClient is the part of the Docker client ReadServiceFile uses.
type ServiceFileClient interface {
	ContainerList(context.Context, dockerclient.ContainerListOptions) (dockerclient.ContainerListResult, error)
	containerfiles.Copier
}

// ReadServiceFile copies one regular file out of the running Docker container
// of a Kurtosis service, such as the chain config its client loads.
func ReadServiceFile(ctx context.Context, client ServiceFileClient, serviceID, filePath string) ([]byte, error) {
	container, err := serviceContainer(ctx, client, serviceID)
	if err != nil {
		return nil, fmt.Errorf("find container: %w", err)
	}
	return containerfiles.ReadFile(ctx, client, container.ID, filePath)
}
