package dockerapi

import (
	"fmt"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

// HostPort returns the host port that a container's TCP port is published to.
func HostPort(inspected containertypes.InspectResponse, port uint16) (string, error) {
	published, _ := network.PortFrom(port, network.TCP)
	var bindings []network.PortBinding
	if settings := inspected.NetworkSettings; settings != nil {
		bindings = settings.Ports[published]
	}
	if len(bindings) == 0 || bindings[0].HostPort == "" {
		return "", fmt.Errorf("port %d is not published", port)
	}
	return bindings[0].HostPort, nil
}
