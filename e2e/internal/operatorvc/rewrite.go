package operatorvc

import (
	"errors"
	"fmt"
	"net"
	"net/url"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

const containerHost = "host.docker.internal"

// RewritePublishedURL rewrites a host-published http(s) URL so a container can
// reach it through host.docker.internal.
func RewritePublishedURL(endpointURL string) (string, error) {
	endpoint, err := url.Parse(endpointURL)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	if endpoint.Scheme == "" || endpoint.Hostname() == "" || endpoint.Port() == "" {
		return "", errors.New("URL must include a scheme, host, and port")
	}
	endpoint.Host = net.JoinHostPort(containerHost, endpoint.Port())
	return endpoint.String(), nil
}

func rewritePublishedHost(address string) (string, error) {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("parse host:port: %w", err)
	}
	if port == "" {
		return "", errors.New("address must include a port")
	}
	return net.JoinHostPort(containerHost, port), nil
}

func publishedHostPort(inspected containertypes.InspectResponse, containerPort uint16) (string, error) {
	if inspected.NetworkSettings == nil {
		return "", errors.New("container has no network settings")
	}
	port, ok := network.PortFrom(containerPort, network.TCP)
	if !ok {
		return "", fmt.Errorf("invalid container port %d", containerPort)
	}
	bindings := inspected.NetworkSettings.Ports[port]
	if len(bindings) == 0 || bindings[0].HostPort == "" {
		return "", fmt.Errorf("container port %d is not published", containerPort)
	}
	return bindings[0].HostPort, nil
}

func gatewayNetworkPort() network.Port {
	port, ok := network.PortFrom(gatewayPort, network.TCP)
	if !ok {
		panic("operator gateway port is invalid")
	}
	return port
}
