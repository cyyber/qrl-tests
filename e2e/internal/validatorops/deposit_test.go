package validatorops

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/chaininfo"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/signing"
	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/accounts/abi"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
)

func TestNewDepositor(t *testing.T) {
	chain := loadChainInfo(t)
	address := common.Address{1}

	t.Run("reads the deposit contract", func(t *testing.T) {
		server := depositContractServer(t, address.String(), http.StatusOK)
		defer server.Close()
		depositor, err := newDepositor(t, server.URL, chain)
		require.NoError(t, err)
		require.Equal(t, address, depositor.address)
		require.Equal(t, chain.DepositDomain(), depositor.domain)
	})

	t.Run("rejects a bad address", func(t *testing.T) {
		server := depositContractServer(t, "not-an-address", http.StatusOK)
		defer server.Close()
		_, err := newDepositor(t, server.URL, chain)
		require.ErrorContains(t, err, "parse deposit contract address")
	})

	t.Run("returns a beacon error", func(t *testing.T) {
		server := depositContractServer(t, address.String(), http.StatusInternalServerError)
		defer server.Close()
		_, err := newDepositor(t, server.URL, chain)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "parse deposit contract address")
	})
}

func depositContractServer(t *testing.T, address string, status int) *httptest.Server {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"data": map[string]string{"chain_id": "1337", "address": address},
	})
	require.NoError(t, err)
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/qrl/v1/config/deposit_contract" {
			http.NotFound(writer, request)
			return
		}
		writer.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = writer.Write(body)
		}
	}))
}

func newDepositor(t *testing.T, beaconURL string, chain chaininfo.Info) (*Depositor, error) {
	t.Helper()
	client, err := beacon.New(beaconURL)
	require.NoError(t, err)
	return NewDepositor(t.Context(), &live.Node{Beacon: client}, chain)
}

func TestDepositInput(t *testing.T) {
	key, err := DeterministicKey(0x91)
	require.NoError(t, err)
	recipientKey, err := DeterministicKey(0x92)
	require.NoError(t, err)
	recipient := recipientKey.Address()
	const amount = uint64(20000000000000)
	domain := signing.ComputeDomain(signing.DomainDeposit, signing.ForkVersion{0x10, 0, 0, 0x20}, signing.Root{})

	data, root, err := depositInput(key, recipient, amount, domain)
	require.NoError(t, err)
	require.Equal(t, key.PublicKey(), data.PublicKey)
	require.Equal(t, recipient.Bytes(), data.WithdrawalRecipient)
	require.Equal(t, amount, data.Amount)
	require.Equal(t, key.RandaoCommitment(), data.RandaoCommitment)

	messageRoot, err := data.DepositMessage.HashTreeRoot()
	require.NoError(t, err)
	require.NoError(t, signing.Verify(signing.SigningRoot(messageRoot, domain), key.PublicKey(), data.Signature))

	dataRoot, err := data.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, dataRoot, root)
}

func TestVerifyDepositEvent(t *testing.T) {
	parsed, err := abi.JSON(strings.NewReader(depositContractABI))
	require.NoError(t, err)
	address := common.Address{1}
	depositor := &Depositor{
		address:  address,
		contract: bind.NewBoundContract(address, parsed, nil, nil, nil),
	}
	data := signing.DepositData{
		DepositMessage: signing.DepositMessage{
			PublicKey:           bytes.Repeat([]byte{0x11}, signing.PublicKeyLength),
			WithdrawalRecipient: bytes.Repeat([]byte{0x22}, signing.WithdrawalRecipientLength),
			Amount:              20000000000000,
			RandaoCommitment:    bytes.Repeat([]byte{0x33}, signing.RandaoCommitmentLength),
		},
		Signature: bytes.Repeat([]byte{0x44}, signing.SignatureLength),
	}
	eventLog := func(logAddress common.Address, publicKey, recipient, commitment, signature, amount []byte) *types.Log {
		t.Helper()
		encoded, err := parsed.Events["DepositEvent"].Inputs.Pack(
			publicKey, recipient, amount, commitment, signature, make([]byte, 8),
		)
		require.NoError(t, err)
		return &types.Log{
			Address: logAddress,
			Topics:  []common.LogTopic{common.HashToLogTopic(parsed.Events["DepositEvent"].ID)},
			Data:    encoded,
		}
	}
	amount := binary.LittleEndian.AppendUint64(nil, data.Amount)
	changed := func(value []byte) []byte {
		out := bytes.Clone(value)
		out[0] ^= 0xff
		return out
	}

	t.Run("amount", func(t *testing.T) {
		for _, test := range []struct {
			name      string
			amount    []byte
			wantError string
		}{
			{"matching", amount, ""},
			{"undercredited", binary.LittleEndian.AppendUint64(nil, data.Amount-1), "deposit event amount is"},
			{"overcredited", binary.LittleEndian.AppendUint64(nil, data.Amount+1), "deposit event amount is"},
			{"empty", nil, "deposit event amount must be 8 bytes"},
			{"short", make([]byte, 7), "deposit event amount must be 8 bytes"},
			{"long", make([]byte, 9), "deposit event amount must be 8 bytes"},
		} {
			t.Run(test.name, func(t *testing.T) {
				log := eventLog(address, data.PublicKey, data.WithdrawalRecipient, data.RandaoCommitment, data.Signature, test.amount)
				err := depositor.verifyEvent(&types.Receipt{Logs: []*types.Log{log}}, data)
				if test.wantError != "" {
					require.ErrorContains(t, err, test.wantError)
				} else {
					require.NoError(t, err)
				}
			})
		}
	})

	t.Run("signed data", func(t *testing.T) {
		for _, test := range []struct {
			name string
			log  *types.Log
		}{
			{"public key", eventLog(address, changed(data.PublicKey), data.WithdrawalRecipient, data.RandaoCommitment, data.Signature, amount)},
			{"withdrawal recipient", eventLog(address, data.PublicKey, changed(data.WithdrawalRecipient), data.RandaoCommitment, data.Signature, amount)},
			{"randao commitment", eventLog(address, data.PublicKey, data.WithdrawalRecipient, changed(data.RandaoCommitment), data.Signature, amount)},
			{"signature", eventLog(address, data.PublicKey, data.WithdrawalRecipient, data.RandaoCommitment, changed(data.Signature), amount)},
		} {
			t.Run(test.name, func(t *testing.T) {
				err := depositor.verifyEvent(&types.Receipt{Logs: []*types.Log{test.log}}, data)
				require.ErrorContains(t, err, "deposit event does not match the signed deposit data")
			})
		}
	})

	t.Run("missing", func(t *testing.T) {
		for _, test := range []struct {
			name string
			logs []*types.Log
		}{
			{"no logs", nil},
			{"other contract", []*types.Log{eventLog(common.Address{2}, data.PublicKey, data.WithdrawalRecipient, data.RandaoCommitment, data.Signature, amount)}},
		} {
			t.Run(test.name, func(t *testing.T) {
				err := depositor.verifyEvent(&types.Receipt{Logs: test.logs}, data)
				require.ErrorContains(t, err, "successful deposit receipt has no deposit event")
			})
		}
	})
}

type chainSource struct{}

func (chainSource) Genesis(context.Context) (beacon.Genesis, error) {
	return beacon.Genesis{ForkVersion: "0x10000020"}, nil
}

func (chainSource) SpecUint(_ context.Context, name string) (uint64, error) {
	switch name {
	case "SLOTS_PER_EPOCH":
		return 8, nil
	case "SECONDS_PER_SLOT":
		return 5, nil
	case "EPOCHS_PER_EXECUTION_VOTING_PERIOD":
		return 64, nil
	case "EXECUTION_FOLLOW_DISTANCE":
		return 8, nil
	case "SECONDS_PER_EXECUTION_BLOCK":
		return 5, nil
	default:
		return 0, fmt.Errorf("unexpected spec value %s", name)
	}
}

func loadChainInfo(t *testing.T) chaininfo.Info {
	t.Helper()
	chain, err := chaininfo.Load(t.Context(), chainSource{})
	require.NoError(t, err)
	return chain
}
