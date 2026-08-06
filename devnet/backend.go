package devnet

import (
	"fmt"
	"strings"
)

type Backend string

type Capability string

const (
	BackendDocker     Backend = "docker"
	BackendKubernetes Backend = "kubernetes"

	CapabilityNetworkPartition Capability = "network-partition"
)

func (backend Backend) Supports(capability Capability) bool {
	switch capability {
	case CapabilityNetworkPartition:
		return backend == BackendDocker
	default:
		return false
	}
}

func ParseBackend(value string) (Backend, error) {
	return Backend(value).normalize()
}

// normalize validates the backend and resolves the empty value to the
// default Docker backend.
func (backend Backend) normalize() (Backend, error) {
	backend = Backend(strings.TrimSpace(string(backend)))
	if backend == "" {
		return BackendDocker, nil
	}
	switch backend {
	case BackendDocker, BackendKubernetes:
		return backend, nil
	default:
		return "", fmt.Errorf("unsupported Kurtosis backend %q", backend)
	}
}

type Images struct {
	Execution string
	Clef      string
	Consensus string
	Validator string
	Genesis   string
}

func DefaultImages() Images {
	return Images{
		Execution: DefaultExecutionImage,
		Clef:      DefaultClefImage,
		Consensus: DefaultConsensusImage,
		Validator: DefaultValidatorImage,
		Genesis:   DefaultGenesisImage,
	}
}

func (images Images) withDefaults() Images {
	defaults := DefaultImages()
	if images.Execution == "" {
		images.Execution = defaults.Execution
	}
	if images.Clef == "" {
		images.Clef = defaults.Clef
	}
	if images.Consensus == "" {
		images.Consensus = defaults.Consensus
	}
	if images.Validator == "" {
		images.Validator = defaults.Validator
	}
	if images.Genesis == "" {
		images.Genesis = defaults.Genesis
	}
	return images
}

func (images Images) validate(backend Backend) error {
	for _, item := range []struct {
		name, image string
	}{
		{"execution", images.Execution},
		{"Clef", images.Clef},
		{"consensus", images.Consensus},
		{"validator", images.Validator},
		{"genesis", images.Genesis},
	} {
		if strings.TrimSpace(item.image) == "" {
			return fmt.Errorf("%s image is empty", item.name)
		}
		if backend == BackendKubernetes && strings.HasPrefix(item.image, "local/") {
			return fmt.Errorf("%s image %q is not available to Kubernetes; use a registry image", item.name, item.image)
		}
	}
	return nil
}
