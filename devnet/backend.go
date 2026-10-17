package devnet

import (
	"fmt"
	"strings"
)

// BackendDocker is the only backend for now.
const BackendDocker Backend = "docker"

type Backend string

// ParseBackend validates the raw value, resolving the empty value to the
// Docker backend; only a verified value becomes a Backend.
func ParseBackend(value string) (Backend, error) {
	switch trimmed := strings.TrimSpace(value); trimmed {
	case "", string(BackendDocker):
		return BackendDocker, nil
	default:
		return "", fmt.Errorf("unsupported Kurtosis backend %q; only %s is supported", value, BackendDocker)
	}
}
