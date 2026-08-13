package devnet

import (
	"cmp"
	"errors"
	"fmt"
	"strings"

	"github.com/distribution/reference"
)

const (
	DefaultExecutionImage = "local/go-qrl:devnet"
	DefaultClefImage      = "local/go-qrl-clef:devnet"
	DefaultConsensusImage = "local/qrysm-beacon:devnet"
	DefaultValidatorImage = "local/qrysm-validator:devnet"
	DefaultGenesisImage   = "local/qrl-genesis-generator:devnet"
)

type Images struct {
	Execution string `json:"execution"`
	Clef      string `json:"clef"`
	Consensus string `json:"consensus"`
	Validator string `json:"validator"`
	Genesis   string `json:"genesis"`
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

// Resolved trims image references, applies defaults, and validates their syntax.
func (images Images) Resolved() (Images, error) {
	var problems []error
	normalize := func(role, value, fallback string) string {
		imageReference := cmp.Or(strings.TrimSpace(value), fallback)
		if _, err := reference.Parse(imageReference); err != nil {
			problems = append(problems, fmt.Errorf("%s image reference %q: %w", role, imageReference, err))
		}
		return imageReference
	}

	resolved := Images{
		Execution: normalize("execution", images.Execution, DefaultExecutionImage),
		Clef:      normalize("Clef", images.Clef, DefaultClefImage),
		Consensus: normalize("consensus", images.Consensus, DefaultConsensusImage),
		Validator: normalize("validator", images.Validator, DefaultValidatorImage),
		Genesis:   normalize("genesis", images.Genesis, DefaultGenesisImage),
	}
	if err := errors.Join(problems...); err != nil {
		return Images{}, err
	}
	return resolved, nil
}
