// Package validatorops builds and submits the operations a QRL staker performs
// over a validator's lifetime: deposits, top-ups, and voluntary exits.
package validatorops

import (
	"github.com/cyyber/qrl-tests/e2e/internal/consensuscrypto"
	"github.com/theQRL/go-qrl/common"
	walletcommon "github.com/theQRL/go-qrllib/wallet/common"
	walletmldsa "github.com/theQRL/go-qrllib/wallet/ml_dsa_87"
)

// Key is a validator signing key. Its RANDAO commitment derives from the same
// seed, exactly as the validator client would rebuild it.
type Key struct {
	wallet *walletmldsa.Wallet
}

// DeterministicKey derives a validator key from a marker byte, so a suite can
// name distinct validators without persisting key material.
func DeterministicKey(marker byte) (*Key, error) {
	raw := make([]byte, walletcommon.SeedSize)
	for index := range raw {
		raw[index] = marker + byte(index)
	}

	seed, err := walletcommon.ToSeed(raw)
	if err != nil {
		return nil, err
	}
	wallet, err := walletmldsa.NewWalletFromSeed(seed)
	if err != nil {
		return nil, err
	}
	return &Key{wallet: wallet}, nil
}

func (key *Key) PublicKey() []byte {
	publicKey := key.wallet.GetPK()
	return publicKey[:]
}

func (key *Key) Seed() []byte {
	seed := key.wallet.GetSeed()
	return seed[:]
}

// Address is the wallet address derived from the validator key.
func (key *Key) Address() common.Address {
	return common.Address(key.wallet.GetAddress())
}

// RandaoCommitment is the top layer of the key's hash onion, committed to in
// the deposit and opened one layer per proposed block.
func (key *Key) RandaoCommitment() []byte {
	seed := key.wallet.GetSeed()
	commitment := consensuscrypto.RandaoCommitment(seed[:])
	return commitment[:]
}

func (key *Key) Sign(message []byte) ([]byte, error) {
	signature, err := key.wallet.Sign(message)
	if err != nil {
		return nil, err
	}
	return signature[:], nil
}
