package signing

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

const WithdrawalRecipientLength = 64

// VoluntaryExit mirrors the consensus VoluntaryExit container.
type VoluntaryExit struct {
	Epoch          uint64
	ValidatorIndex uint64
}

func (exit VoluntaryExit) HashTreeRoot() ([RootLength]byte, error) {
	return containerRoot(uint64Root(exit.Epoch), uint64Root(exit.ValidatorIndex)), nil
}

// DepositMessage is the unsigned part of a deposit: what the validator key
// signs under the deposit domain.
type DepositMessage struct {
	PublicKey           []byte
	WithdrawalRecipient []byte
	Amount              uint64
	RandaoCommitment    []byte
}

func (message DepositMessage) HashTreeRoot() ([RootLength]byte, error) {
	fields, err := message.fieldRoots()
	if err != nil {
		return [RootLength]byte{}, err
	}
	return containerRoot(fields...), nil
}

func (message DepositMessage) fieldRoots() ([][RootLength]byte, error) {
	publicKeyRoot, err := fixedBytesRoot("deposit public key", message.PublicKey, PublicKeyLength)
	if err != nil {
		return nil, err
	}
	recipientRoot, err := fixedBytesRoot("withdrawal recipient", message.WithdrawalRecipient, WithdrawalRecipientLength)
	if err != nil {
		return nil, err
	}
	commitmentRoot, err := fixedBytesRoot("randao commitment", message.RandaoCommitment, RandaoCommitmentLength)
	if err != nil {
		return nil, err
	}
	return [][RootLength]byte{publicKeyRoot, recipientRoot, uint64Root(message.Amount), commitmentRoot}, nil
}

// DepositData is the signed deposit whose root the deposit contract checks.
type DepositData struct {
	DepositMessage
	Signature []byte
}

func (data DepositData) HashTreeRoot() ([RootLength]byte, error) {
	fields, err := data.fieldRoots()
	if err != nil {
		return [RootLength]byte{}, err
	}
	signature, err := fixedBytesRoot("deposit signature", data.Signature, SignatureLength)
	if err != nil {
		return [RootLength]byte{}, err
	}
	return containerRoot(append(fields, signature)...), nil
}

func uint64Root(value uint64) [RootLength]byte {
	var root [RootLength]byte
	binary.LittleEndian.PutUint64(root[:8], value)
	return root
}

func fixedBytesRoot(name string, value []byte, length int) ([RootLength]byte, error) {
	if len(value) != length {
		return [RootLength]byte{}, fmt.Errorf("%s must be %d bytes, got %d", name, length, len(value))
	}
	chunks := make([][RootLength]byte, (length+RootLength-1)/RootLength)
	for index := range chunks {
		copy(chunks[index][:], value[index*RootLength:])
	}
	return merkleize(chunks), nil
}

func containerRoot(fields ...[RootLength]byte) [RootLength]byte {
	return merkleize(fields)
}

func merkleize(chunks [][RootLength]byte) [RootLength]byte {
	width := 1
	for width < len(chunks) {
		width *= 2
	}
	level := make([][RootLength]byte, width)
	copy(level, chunks)
	for width > 1 {
		for index := 0; index < width; index += 2 {
			var pair [RootLength * 2]byte
			copy(pair[:RootLength], level[index][:])
			copy(pair[RootLength:], level[index+1][:])
			level[index/2] = sha256.Sum256(pair[:])
		}
		width /= 2
		level = level[:width]
	}
	return level[0]
}
