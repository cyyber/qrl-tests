// Package signing implements the domain, root, and signature formulas used to
// build and independently verify QRL consensus signatures.
package signing

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	walletmldsa "github.com/theQRL/go-qrllib/wallet/ml_dsa_87"
)

const (
	RootLength      = sha256.Size
	PublicKeyLength = walletmldsa.PKSize
	SignatureLength = walletmldsa.SigSize
)

var (
	DomainBeaconProposer = DomainType{0x00, 0x00, 0x00, 0x00}
	DomainBeaconAttester = DomainType{0x01, 0x00, 0x00, 0x00}
	DomainDeposit        = DomainType{0x03, 0x00, 0x00, 0x00}
)

type (
	DomainType  [4]byte
	ForkVersion [4]byte
	Root        [RootLength]byte
	Domain      [RootLength]byte
)

// Fork is a state's fork: the version it signs with from Epoch on, and the
// one before.
type Fork struct {
	PreviousVersion ForkVersion
	CurrentVersion  ForkVersion
	Epoch           uint64
}

// Domain mirrors get_domain for a state with this fork: messages of earlier
// epochs keep the previous version.
func (fork Fork) Domain(domainType DomainType, epoch uint64, genesisValidatorsRoot Root) Domain {
	version := fork.CurrentVersion
	if epoch < fork.Epoch {
		version = fork.PreviousVersion
	}
	return ComputeDomain(domainType, version, genesisValidatorsRoot)
}

// ParseRoot decodes a 0x-prefixed 32-byte root.
func ParseRoot(value string) (Root, error) {
	var root Root
	return root, decodeFixed("root", value, root[:])
}

// ParseForkVersion decodes a 0x-prefixed 4-byte fork version.
func ParseForkVersion(value string) (ForkVersion, error) {
	var version ForkVersion
	return version, decodeFixed("fork version", value, version[:])
}

func decodeFixed(name, value string, result []byte) error {
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil {
		return fmt.Errorf("decode %s %q: %w", name, value, err)
	}
	if len(decoded) != len(result) {
		return fmt.Errorf("%s %q is %d bytes, want %d", name, value, len(decoded), len(result))
	}
	copy(result, decoded)
	return nil
}

// ComputeDomain mirrors compute_domain: the domain type followed by the first
// 28 bytes of the fork data root.
func ComputeDomain(domainType DomainType, forkVersion ForkVersion, genesisValidatorsRoot Root) Domain {
	var version Root
	copy(version[:4], forkVersion[:])
	forkDataRoot := merkleize([]Root{version, genesisValidatorsRoot})

	var domain Domain
	copy(domain[:4], domainType[:])
	copy(domain[4:], forkDataRoot[:RootLength-4])
	return domain
}

// SigningRoot mirrors compute_signing_root for an object root and domain.
func SigningRoot(objectRoot Root, domain Domain) Root {
	return merkleize([]Root{objectRoot, Root(domain)})
}

// Verify checks an ML-DSA-87 signature over message with the given public key.
func Verify(message Root, publicKey, signature []byte) error {
	key, err := walletmldsa.BytesToPK(publicKey)
	if err != nil {
		return err
	}
	if len(signature) != SignatureLength {
		return fmt.Errorf("signature must be %d bytes, got %d", SignatureLength, len(signature))
	}
	descriptor, err := walletmldsa.NewMLDSA87Descriptor()
	if err != nil {
		return err
	}
	if !walletmldsa.Verify(message[:], signature, &key, descriptor.ToDescriptor()) {
		return errors.New("signature did not verify")
	}
	return nil
}
