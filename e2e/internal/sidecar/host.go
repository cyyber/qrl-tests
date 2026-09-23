package sidecar

import (
	"fmt"
	"net"
	"net/url"
)

// containerHost resolves to the Docker host from inside a sidecar. Docker
// Desktop defines it; on Linux it only resolves because every sidecar is
// created with a host-gateway mapping for it.
const containerHost = "host.docker.internal"

// HostURL rewrites a host-published URL, such as an HTTP or WebSocket endpoint,
// so a sidecar can reach it.
func HostURL(endpointURL string) (string, error) {
	endpoint, err := url.Parse(endpointURL)
	if err != nil {
		return "", err
	}
	if endpoint.Scheme == "" || endpoint.Hostname() == "" || endpoint.Port() == "" {
		return "", fmt.Errorf("URL %q must include a scheme, host, and port", endpointURL)
	}
	endpoint.Host = net.JoinHostPort(containerHost, endpoint.Port())
	return endpoint.String(), nil
}

// HostAddress rewrites a host-published host:port so a sidecar can reach it.
func HostAddress(address string) (string, error) {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("parse host:port: %w", err)
	}
	if port == "" {
		return "", fmt.Errorf("address %q must include a port", address)
	}
	return net.JoinHostPort(containerHost, port), nil
}
