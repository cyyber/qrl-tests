// Package consensuscrypto implements the protocol-level domain and signing
// root formulas used to build and independently verify QRL consensus
// signatures.
package consensuscrypto

import (
	"crypto/sha256"
	"errors"
	"fmt"

	walletmldsa "github.com/theQRL/go-qrllib/wallet/ml_dsa_87"
)

const RootLength = sha256.Size

var (
	DomainDeposit       = [4]byte{0x03, 0x00, 0x00, 0x00}
	DomainVoluntaryExit = [4]byte{0x04, 0x00, 0x00, 0x00}
)

// ComputeDomain mirrors compute_domain: the domain type followed by the first
// 28 bytes of the fork data root.
func ComputeDomain(domainType [4]byte, forkVersion [4]byte, genesisValidatorsRoot [RootLength]byte) [RootLength]byte {
	var version [RootLength]byte
	copy(version[:4], forkVersion[:])
	forkDataRoot := containerRoot(version, genesisValidatorsRoot)

	var domain [RootLength]byte
	copy(domain[:4], domainType[:])
	copy(domain[4:], forkDataRoot[:RootLength-4])
	return domain
}

// SigningRoot mirrors compute_signing_root for an object root and domain.
func SigningRoot(objectRoot, domain [RootLength]byte) [RootLength]byte {
	return containerRoot(objectRoot, domain)
}

// Verify checks an ML-DSA-87 signature over message with the given public key.
func Verify(message [RootLength]byte, publicKey, signature []byte) error {
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
