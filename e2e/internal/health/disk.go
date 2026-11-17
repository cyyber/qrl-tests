package health

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	dockerclient "github.com/moby/moby/client"
)

// DiskUsage returns the bytes under path in a running container, as du counts
// them. The container's image must have du.
func DiskUsage(ctx context.Context, docker sidecar.Executor, container, path string) (uint64, error) {
	output, err := sidecar.Exec(ctx, docker, container, "du", "-sk", path)
	// du fails on a file the node removes while du walks the directory, such
	// as a database's compacted table, but it still prints the total.
	for line := range strings.Lines(output) {
		kilobytes, found := strings.CutSuffix(strings.TrimRight(line, "\n"), "\t"+path)
		if !found {
			continue
		}
		total, parseErr := strconv.ParseUint(kilobytes, 10, 64)
		if parseErr != nil {
			return 0, fmt.Errorf("du of %s in %s: %w", path, container, parseErr)
		}
		return total << 10, nil
	}
	if err != nil {
		return 0, fmt.Errorf("du of %s in %s: %w", path, container, err)
	}
	return 0, fmt.Errorf("du of %s in %s printed no total: %q", path, container, output)
}

// ContainerInspector reads a container's details.
type ContainerInspector interface {
	ContainerInspect(context.Context, string, dockerclient.ContainerInspectOptions) (dockerclient.ContainerInspectResult, error)
}

// WritableLayerSize returns the bytes a container has written over its image:
// a node's data, when its data directory is not a volume. It suits an image
// without du, such as qrysm's distroless ones.
func WritableLayerSize(ctx context.Context, docker ContainerInspector, container string) (uint64, error) {
	inspected, err := docker.ContainerInspect(ctx, container, dockerclient.ContainerInspectOptions{Size: true})
	if err != nil {
		return 0, fmt.Errorf("inspect %s: %w", container, err)
	}
	size := inspected.Container.SizeRw
	if size == nil || *size < 0 {
		return 0, errors.New("docker did not size the writable layer of " + container)
	}
	return uint64(*size), nil
}
