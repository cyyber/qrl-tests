// Package signing implements the domain, root, and signature formulas used to
// build and independently verify QRL consensus signatures.
package signing

import (
	"crypto/sha256"
	"errors"
	"fmt"

	walletmldsa "github.com/theQRL/go-qrllib/wallet/ml_dsa_87"
)

const (
	RootLength      = sha256.Size
	PublicKeyLength = walletmldsa.PKSize
	SignatureLength = walletmldsa.SigSize
)

var (
	DomainDeposit       = DomainType{0x03, 0x00, 0x00, 0x00}
	DomainVoluntaryExit = DomainType{0x04, 0x00, 0x00, 0x00}
)

type (
	DomainType  [4]byte
	ForkVersion [4]byte
	Root        [RootLength]byte
	Domain      [RootLength]byte
)

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
