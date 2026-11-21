package dockerapi

import (
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
)

func TestHostPort(t *testing.T) {
	port, _ := network.PortFrom(7500, network.TCP)
	inspected := func(bindings ...network.PortBinding) containertypes.InspectResponse {
		return containertypes.InspectResponse{NetworkSettings: &containertypes.NetworkSettings{
			Ports: network.PortMap{port: bindings},
		}}
	}

	hostPort, err := HostPort(inspected(network.PortBinding{HostPort: "32000"}), 7500)
	require.NoError(t, err)
	require.Equal(t, "32000", hostPort)

	for name, response := range map[string]containertypes.InspectResponse{
		"no network settings": {},
		"no binding":          inspected(),
		"no host port":        inspected(network.PortBinding{}),
	} {
		_, err := HostPort(response, 7500)
		require.EqualError(t, err, "port 7500 is not published", name)
	}
}
