package devnet

import (
	"cmp"
	"errors"
	"fmt"
	"regexp"
	"strings"
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

// Resolved trims every reference, falls back to the local development
// defaults for blank ones, and rejects references no registry could serve,
// reporting every invalid reference at once so CI surfaces all mistakes in
// one run.
func (images Images) Resolved() (Images, error) {
	var problems []error
	normalize := func(role, value, fallback string) string {
		reference := cmp.Or(strings.TrimSpace(value), fallback)
		if err := validateImageReference(reference); err != nil {
			problems = append(problems, fmt.Errorf("%s image: %w", role, err))
		}
		return reference
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

// The reference grammar is the Docker distribution subset the profiles use:
// name[:tag][@algorithm:hex], where a name is an optional registry host
// followed by lowercase path components.
var (
	imageDomainPattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)*(?::[0-9]+)?$`)
	imagePathPattern   = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*)*$`)
	imageTagPattern    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
	digestPattern      = regexp.MustCompile(`^[a-z0-9]+(?:[+._-][a-z0-9]+)*:[0-9a-fA-F]{32,}$`)
)

func validateImageReference(reference string) error {
	name := reference
	if index := strings.IndexByte(name, '@'); index >= 0 {
		if err := validateImageDigest(name[index+1:]); err != nil {
			return fmt.Errorf("reference %q: %w", reference, err)
		}
		name = name[:index]
	}

	if index := strings.LastIndexByte(name, ':'); index > strings.LastIndexByte(name, '/') {
		if tag := name[index+1:]; !imageTagPattern.MatchString(tag) {
			return fmt.Errorf("reference %q: invalid tag %q", reference, tag)
		}
		name = name[:index]
	}

	// The first segment is a registry host exactly when it could not be a
	// repository component: only then may it contain dots, a port, or both.
	path := name
	if head, rest, found := strings.Cut(name, "/"); found && (strings.ContainsAny(head, ".:") || head == "localhost") {
		if !imageDomainPattern.MatchString(head) {
			return fmt.Errorf("reference %q: invalid registry host %q", reference, head)
		}
		path = rest
	}
	if !imagePathPattern.MatchString(path) {
		return fmt.Errorf("reference %q: invalid name %q", reference, path)
	}
	return nil
}

func validateImageDigest(digest string) error {
	if !digestPattern.MatchString(digest) {
		return fmt.Errorf("invalid digest %q", digest)
	}
	if hex, found := strings.CutPrefix(digest, "sha256:"); found && len(hex) != 64 {
		return fmt.Errorf("sha256 digest %q must contain 64 hexadecimal characters", hex)
	}
	return nil
}
