package devnet

import (
	"fmt"
	"strings"
)

const (
	BackendDocker     Backend = "docker"
	BackendKubernetes Backend = "kubernetes"
)

type Backend string

// ParseBackend validates the raw value, resolving the empty value to the
// default Docker backend; only a verified value becomes a Backend.
func ParseBackend(value string) (Backend, error) {
	switch trimmed := strings.TrimSpace(value); trimmed {
	case "":
		return BackendDocker, nil
	case string(BackendDocker), string(BackendKubernetes):
		return Backend(trimmed), nil
	default:
		return "", fmt.Errorf("unsupported Kurtosis backend %q", value)
	}
}
