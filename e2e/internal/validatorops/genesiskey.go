package validatorops

import (
	"context"
	"fmt"
	"strings"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/theQRL/go-qrl/common/hexutil"
	walletcommon "github.com/theQRL/go-qrllib/wallet/common"
	walletmisc "github.com/theQRL/go-qrllib/wallet/misc"
	walletmldsa "github.com/theQRL/go-qrllib/wallet/ml_dsa_87"
	"golang.org/x/crypto/sha3"
)

// GenesisKey derives the key of the validator at index from mnemonic as
// qrysm's deposit CLI does, without a container: SHAKE-256 of the mnemonic's
// seed and the path m/12381/238/<index>/0.
func GenesisKey(mnemonic string, index uint64) (*Key, error) {
	root, err := walletmisc.MnemonicToBin(mnemonic)
	if err != nil {
		return nil, fmt.Errorf("decode mnemonic: %w", err)
	}
	if len(root) != walletcommon.SeedSize {
		return nil, fmt.Errorf("the mnemonic holds %d bytes, not a seed of %d", len(root), walletcommon.SeedSize)
	}
	hash := sha3.NewShake256()
	_, _ = hash.Write(root)
	_, _ = fmt.Fprintf(hash, "m/12381/238/%d/0", index)
	var seed walletcommon.Seed
	_, _ = hash.Read(seed[:])
	wallet, err := walletmldsa.NewWalletFromSeed(seed)
	if err != nil {
		return nil, err
	}
	return &Key{wallet: wallet}, nil
}

// GenesisKeys derives the keys of the genesis validators at indices from
// devnet.GenesisMnemonic, and checks that the head state holds each validator
// by its derived key, active.
func GenesisKeys(ctx context.Context, client *beacon.Client, indices []uint64) ([]*Key, error) {
	records, err := client.ValidatorsAt(ctx, "head", indices)
	if err != nil {
		return nil, err
	}
	keys := make([]*Key, len(indices))
	for position, index := range indices {
		keys[position], err = GenesisKey(devnet.GenesisMnemonic, index)
		if err != nil {
			return nil, fmt.Errorf("derive genesis validator %d: %w", index, err)
		}
		record := records[position]
		if !strings.EqualFold(record.PublicKey, hexutil.Encode(keys[position].PublicKey())) {
			return nil, fmt.Errorf("genesis validator %d is not the key derived for its index", index)
		}
		if record.Status != "active_ongoing" {
			return nil, fmt.Errorf("genesis validator %d is %s, not active_ongoing", index, record.Status)
		}
	}
	return keys, nil
}
