package devnet

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/cyyber/qrl-tests/internal/dockerapi"
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
	return resolveContainerImage(ctx, id, role, client.ContainerList)
}

func primaryExecutionServiceID(environment Environment) (string, error) {
	return primaryServiceID(environment, "execution", func(participant Participant) string {
		return participant.Execution.ID
	})
}

func primaryValidatorServiceID(environment Environment) (string, error) {
	return primaryServiceID(environment, "validator", func(participant Participant) string {
		return participant.Validator.ID
	})
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

func resolveExecutionImage(
	ctx context.Context,
	serviceID string,
	listContainers func(
		context.Context,
		dockerclient.ContainerListOptions,
	) (dockerclient.ContainerListResult, error),
) (string, error) {
	return resolveContainerImage(ctx, serviceID, "execution", listContainers)
}

func resolveContainerImage(
	ctx context.Context,
	serviceID, role string,
	listContainers func(
		context.Context,
		dockerclient.ContainerListOptions,
	) (dockerclient.ContainerListResult, error),
) (string, error) {
	containers, err := listContainers(ctx, dockerclient.ContainerListOptions{
		Filters: make(dockerclient.Filters).Add(
			"label",
			kurtosisServiceUUIDDockerLabel+"="+serviceID,
		),
	})
	if err != nil {
		return "", fmt.Errorf("find primary %s container: %w", role, err)
	}
	if len(containers.Items) != 1 {
		return "", fmt.Errorf(
			"expected one running Docker container for service %q, found %d",
			serviceID,
			len(containers.Items),
		)
	}

	imageID := strings.TrimSpace(containers.Items[0].ImageID)
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
