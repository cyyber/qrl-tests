package devnet

import (
	"fmt"
	"strings"
)

type Backend string

const (
	BackendDocker     Backend = "docker"
	BackendKubernetes Backend = "kubernetes"
)

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
