package devnet

import (
	"fmt"
	"strings"
)

// EndpointMode selects how service endpoints are resolved.
//
// Public endpoints go through what Kurtosis exposes to the caller: Docker
// published ports, or the gateway's port-forwards on Kubernetes. Cluster
// endpoints are the services' private addresses, reachable only from inside
// the same network: the Docker enclave network, or the Kubernetes cluster.
// A Job running in-cluster uses cluster mode so a multi-hour run does not
// depend on long-lived port-forwards.
type EndpointMode string

const (
	EndpointModePublic  EndpointMode = "public"
	EndpointModeCluster EndpointMode = "cluster"
)

// ParseEndpointMode validates the raw value, resolving the empty value to
// public endpoints.
func ParseEndpointMode(value string) (EndpointMode, error) {
	switch trimmed := strings.TrimSpace(value); trimmed {
	case "":
		return EndpointModePublic, nil
	case string(EndpointModePublic), string(EndpointModeCluster):
		return EndpointMode(trimmed), nil
	default:
		return "", fmt.Errorf("unsupported endpoint mode %q", value)
	}
}
