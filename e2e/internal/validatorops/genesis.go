package validatorops

import (
	"context"
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
	derived, err := deposit.GenesisKey(ctx, deposit.GenesisKeyConfig{
		Image:               image,
		Mnemonic:            devnet.GenesisMnemonic,
		Index:               index,
		WithdrawalRecipient: withdrawalRecipient,
		Password:            KeystorePassword,
	})
	if err != nil {
		return nil, err
	}
	record, err := client.Validator(ctx, strconv.FormatUint(index, 10))
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(record.PublicKey, derived.PublicKey) {
		return nil, fmt.Errorf("genesis validator %d is not the key derived for its index", index)
	}
	if record.Status != "active_ongoing" {
		return nil, fmt.Errorf("genesis validator %d is %s, not active_ongoing", index, record.Status)
	}
	return derived.Keystores, nil
}
