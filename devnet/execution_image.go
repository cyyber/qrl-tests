package devnet

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/cyyber/qrl-tests/internal/dockerapi"
	dockerclient "github.com/moby/moby/client"
)

const kurtosisServiceUUIDDockerLabel = "com.kurtosistech.guid"

// ResolveExecutionImage returns the immutable Docker image ID used by the
// primary execution service's running container.
func ResolveExecutionImage(ctx context.Context, environment Environment) (string, error) {
	serviceID, err := primaryExecutionServiceID(environment)
	if err != nil {
		return "", err
	}
	client, err := dockerapi.New()
	if err != nil {
		return "", fmt.Errorf("create Docker client: %w", err)
	}
	defer func() { _ = client.Close() }()
	return resolveExecutionImage(ctx, serviceID, client.ContainerList)
}

func primaryExecutionServiceID(environment Environment) (string, error) {
	if environment.Backend != BackendDocker {
		return "", fmt.Errorf("backend %q is not Docker", environment.Backend)
	}
	primary, err := environment.Primary()
	if err != nil {
		return "", fmt.Errorf("select primary participant: %w", err)
	}
	serviceID := strings.TrimSpace(primary.Execution.ID)
	if serviceID == "" {
		return "", errors.New("primary execution service has no ID")
	}
	return serviceID, nil
}

func resolveExecutionImage(
	ctx context.Context,
	serviceID string,
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
		return "", fmt.Errorf("find primary execution container: %w", err)
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
