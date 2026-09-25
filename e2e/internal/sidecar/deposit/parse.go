package deposit

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
)

const (
	depositDataFilePrefix = "deposit_data-"
	keystoreFilePrefix    = "keystore-"
)

type depositData struct {
	PubKey              string `json:"pubkey"`
	Amount              uint64 `json:"amount"`
	WithdrawalRecipient string `json:"withdrawal_recipient"`
	RandaoCommitment    string `json:"randao_commitment"`
}

func parseDepositOutput(files []sidecar.File) (Result, error) {
	var (
		dataFiles []sidecar.File
		keystores []sidecar.File
	)
	for _, file := range files {
		name := path.Base(file.Name)
		switch {
		case strings.HasPrefix(name, depositDataFilePrefix) && strings.HasSuffix(name, ".json"):
			dataFiles = append(dataFiles, sidecar.File{Name: name, Body: file.Body})
		case strings.HasPrefix(name, keystoreFilePrefix) && strings.HasSuffix(name, ".json"):
			keystores = append(keystores, sidecar.File{Name: name, Body: file.Body})
		}
	}
	if len(dataFiles) != 1 {
		return Result{}, fmt.Errorf("expected one deposit data file, found %d", len(dataFiles))
	}
	if len(keystores) != 1 {
		return Result{}, fmt.Errorf("expected one keystore, found %d", len(keystores))
	}

	var entries []depositData
	if err := json.Unmarshal(dataFiles[0].Body, &entries); err != nil {
		return Result{}, fmt.Errorf("decode deposit data: %w", err)
	}
	if len(entries) != 1 {
		return Result{}, fmt.Errorf("expected one deposit data entry, found %d", len(entries))
	}
	entry := entries[0]
	if entry.PubKey == "" || entry.WithdrawalRecipient == "" || entry.Amount == 0 {
		return Result{}, errors.New("deposit data is missing pubkey, withdrawal recipient, or amount")
	}
	return Result{
		PublicKey:        entry.PubKey,
		Amount:           entry.Amount,
		RandaoCommitment: entry.RandaoCommitment,
		Keystores:        keystores,
	}, nil
}
