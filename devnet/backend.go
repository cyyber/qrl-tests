package devnet

import (
	"cmp"
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

const (
	DefaultExecutionImage = "local/go-qrl:devnet"
	DefaultClefImage      = "local/go-qrl-clef:devnet"
	DefaultConsensusImage = "local/qrysm-beacon:devnet"
	DefaultValidatorImage = "local/qrysm-validator:devnet"
	DefaultGenesisImage   = "local/qrl-genesis-generator:devnet"
)

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

// withDefaults trims every reference and resolves blank ones to the local
// development defaults.
func (images Images) withDefaults() Images {
	normalize := func(value, fallback string) string {
		return cmp.Or(strings.TrimSpace(value), fallback)
	}
	return Images{
		Execution: normalize(images.Execution, DefaultExecutionImage),
		Clef:      normalize(images.Clef, DefaultClefImage),
		Consensus: normalize(images.Consensus, DefaultConsensusImage),
		Validator: normalize(images.Validator, DefaultValidatorImage),
		Genesis:   normalize(images.Genesis, DefaultGenesisImage),
	}
}
