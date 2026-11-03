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

	// DefaultRemoteSignerImage is go-remotesigner's consensus-only image,
	// built from its own Dockerfile by scripts/build-remote-signer-image.sh.
	DefaultRemoteSignerImage = "local/go-remotesigner:devnet"
	// DefaultRemoteSignerExecutionImage is go-remotesigner's full image, the
	// flavour with execution mode, built by
	// scripts/build-remote-signer-image.sh with REMOTESIGNER_FLAVOUR=remotesigner.
	DefaultRemoteSignerExecutionImage = "local/go-remotesigner:devnet-full"

	// DefaultQrysmAlltoolsImage is not an image of the network: sidecars run
	// Qrysm's command-line tools from it.
	DefaultQrysmAlltoolsImage = "local/qrysm-alltools:devnet"
)

type Images struct {
	Execution string `json:"execution"`
	Clef      string `json:"clef"`
	Consensus string `json:"consensus"`
	Validator string `json:"validator"`
	Genesis   string `json:"genesis"`

	// RemoteSigner signs the validator duties of the remote-signer profile.
	RemoteSigner string `json:"remote_signer"`
	// RemoteSignerExecution fronts the execution client of the
	// remote-signer-execution profile.
	RemoteSignerExecution string `json:"remote_signer_execution"`
}

func DefaultImages() Images {
	return Images{
		Execution: DefaultExecutionImage,
		Clef:      DefaultClefImage,
		Consensus: DefaultConsensusImage,
		Validator: DefaultValidatorImage,
		Genesis:   DefaultGenesisImage,

		RemoteSigner:          DefaultRemoteSignerImage,
		RemoteSignerExecution: DefaultRemoteSignerExecutionImage,
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

		RemoteSigner: normalize("remote signer", images.RemoteSigner, DefaultRemoteSignerImage),
		RemoteSignerExecution: normalize("execution remote signer", images.RemoteSignerExecution,
			DefaultRemoteSignerExecutionImage),
	}
	if err := errors.Join(problems...); err != nil {
		return Images{}, err
	}
	return resolved, nil
}
