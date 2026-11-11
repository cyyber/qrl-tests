// Package interchange builds and reads slashing-protection histories in the
// EIP-3076 interchange format, in version 5, which Qrysm imports and exports
// with ML-DSA-87 public keys.
package interchange

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cyyber/qrl-tests/e2e/internal/signing"
)

const FormatVersion = "5"

// Interchange is the slashing-protection history of one or more keys.
type Interchange struct {
	Metadata Metadata `json:"metadata"`
	Data     []Record `json:"data"`
}

type Metadata struct {
	FormatVersion         string `json:"interchange_format_version"`
	GenesisValidatorsRoot string `json:"genesis_validators_root"`
}

// Record is what one key has signed. EIP-3076 lets a key have more than one.
type Record struct {
	PublicKey          string        `json:"pubkey"`
	SignedBlocks       []Block       `json:"signed_blocks"`
	SignedAttestations []Attestation `json:"signed_attestations"`
}

// Block is a signed block. Its signing root is optional.
type Block struct {
	Slot        uint64 `json:"slot,string"`
	SigningRoot string `json:"signing_root,omitempty"`
}

// Attestation is a signed attestation. Its signing root is optional.
type Attestation struct {
	SourceEpoch uint64 `json:"source_epoch,string"`
	TargetEpoch uint64 `json:"target_epoch,string"`
	SigningRoot string `json:"signing_root,omitempty"`
}

// ConsecutiveAttestations is the history of a key that attested for every
// target from first to last, each with the previous epoch as its source, as a
// client on a healthy chain records it. The signing roots are left out, so a
// client must refuse any attestation for those targets.
func ConsecutiveAttestations(genesisValidatorsRoot, publicKey string, first, last uint64) (Interchange, error) {
	if err := checkHex("genesis validators root", genesisValidatorsRoot, signing.RootLength); err != nil {
		return Interchange{}, err
	}
	if err := checkHex("public key", publicKey, signing.PublicKeyLength); err != nil {
		return Interchange{}, err
	}
	if first == 0 {
		return Interchange{}, errors.New("the first target epoch must be after epoch 0, its source")
	}
	if last < first {
		return Interchange{}, fmt.Errorf("the last target epoch %d is before the first, %d", last, first)
	}
	attestations := make([]Attestation, 0, last-first+1)
	for target := first; target <= last; target++ {
		attestations = append(attestations, Attestation{SourceEpoch: target - 1, TargetEpoch: target})
	}
	return Interchange{
		Metadata: Metadata{FormatVersion: FormatVersion, GenesisValidatorsRoot: genesisValidatorsRoot},
		Data: []Record{{
			PublicKey:          publicKey,
			SignedBlocks:       []Block{},
			SignedAttestations: attestations,
		}},
	}, nil
}

// Parse decodes a history and checks its format version.
func Parse(raw string) (Interchange, error) {
	var history Interchange
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		return Interchange{}, fmt.Errorf("decode slashing-protection history: %w", err)
	}
	if history.Metadata.FormatVersion != FormatVersion {
		return Interchange{}, fmt.Errorf("slashing-protection history has format version %q, want %q",
			history.Metadata.FormatVersion, FormatVersion)
	}
	return history, nil
}

// Encode returns the history as the JSON string the keymanager API takes.
func (history Interchange) Encode() (string, error) {
	encoded, err := json.Marshal(history)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// Attestations returns every attestation the history holds for publicKey,
// matched ignoring case and the 0x prefix.
func (history Interchange) Attestations(publicKey string) []Attestation {
	var attestations []Attestation
	for _, record := range history.Data {
		if SameHex(record.PublicKey, publicKey) {
			attestations = append(attestations, record.SignedAttestations...)
		}
	}
	return attestations
}

// SameHex reports whether two hex strings are equal, ignoring case and the 0x
// prefix.
func SameHex(first, second string) bool {
	return strings.EqualFold(strings.TrimPrefix(first, "0x"), strings.TrimPrefix(second, "0x"))
}

func checkHex(name, value string, length int) error {
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	if len(decoded) != length {
		return fmt.Errorf("%s is %d bytes, want %d", name, len(decoded), length)
	}
	return nil
}
