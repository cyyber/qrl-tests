package depositcli

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/cyyber/qrl-tests/e2e/internal/operatorvc"
)

const depositDataFilePrefix = "deposit_data-"

type depositData struct {
	PubKey              string `json:"pubkey"`
	Amount              uint64 `json:"amount"`
	WithdrawalRecipient string `json:"withdrawal_recipient"`
	RandaoCommitment    string `json:"randao_commitment"`
}

func parseDepositOutput(files []operatorvc.File) (Result, error) {
	var (
		dataFiles []operatorvc.File
		keystores []operatorvc.File
	)
	for _, file := range files {
		name := path.Base(file.Name)
		switch {
		case strings.HasPrefix(name, depositDataFilePrefix) && strings.HasSuffix(name, ".json"):
			dataFiles = append(dataFiles, operatorvc.File{Name: name, Body: file.Body})
		case strings.HasPrefix(name, "keystore-") && strings.HasSuffix(name, ".json"):
			keystores = append(keystores, operatorvc.File{Name: name, Body: file.Body})
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
		PublicKey:           entry.PubKey,
		Amount:              entry.Amount,
		WithdrawalRecipient: entry.WithdrawalRecipient,
		RandaoCommitment:    entry.RandaoCommitment,
		Keystores:           keystores,
	}, nil
}

func readTarFiles(reader io.Reader) ([]operatorvc.File, error) {
	archive := tar.NewReader(reader)
	var files []operatorvc.File
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		body, err := io.ReadAll(archive)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", header.Name, err)
		}
		files = append(files, operatorvc.File{Name: path.Base(header.Name), Body: body})
	}
}
