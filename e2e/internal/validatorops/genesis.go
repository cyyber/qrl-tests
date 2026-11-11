package validatorops

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/deposit"
)

// GenesisKeystores derives the keystores of the genesis validator at index,
// encrypted with KeystorePassword, and checks that the beacon node knows that
// validator by the derived key and that it is active.
func GenesisKeystores(
	ctx context.Context, client *beacon.Client, image, withdrawalRecipient string, index uint64,
) ([]sidecar.File, error) {
	derived, err := genesisKey(ctx, client, image, withdrawalRecipient, index)
	return derived.Keystores, err
}

// GenesisKey derives the key of the active genesis validator at index, as
// GenesisKeystores does, so a suite can sign the validator's messages itself.
func GenesisKey(ctx context.Context, client *beacon.Client, image, withdrawalRecipient string, index uint64) (*Key, error) {
	derived, err := genesisKey(ctx, client, image, withdrawalRecipient, index)
	if err != nil {
		return nil, err
	}
	if len(derived.Keystores) != 1 {
		return nil, fmt.Errorf("derived %d keystores for genesis validator %d, want 1", len(derived.Keystores), index)
	}
	key, err := KeyFromKeystore(derived.Keystores[0].Body, KeystorePassword)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold("0x"+hex.EncodeToString(key.PublicKey()), derived.PublicKey) {
		return nil, fmt.Errorf("the keystore of genesis validator %d holds another key", index)
	}
	return key, nil
}

func genesisKey(
	ctx context.Context, client *beacon.Client, image, withdrawalRecipient string, index uint64,
) (deposit.Result, error) {
	derived, err := deposit.GenesisKey(ctx, deposit.GenesisKeyConfig{
		Image:               image,
		Mnemonic:            devnet.GenesisMnemonic,
		Index:               index,
		WithdrawalRecipient: withdrawalRecipient,
		Password:            KeystorePassword,
	})
	if err != nil {
		return deposit.Result{}, err
	}
	record, err := client.Validator(ctx, strconv.FormatUint(index, 10))
	if err != nil {
		return deposit.Result{}, err
	}
	if !strings.EqualFold(record.PublicKey, derived.PublicKey) {
		return deposit.Result{}, fmt.Errorf("genesis validator %d is not the key derived for its index", index)
	}
	if record.Status != "active_ongoing" {
		return deposit.Result{}, fmt.Errorf("genesis validator %d is %s, not active_ongoing", index, record.Status)
	}
	return derived, nil
}
