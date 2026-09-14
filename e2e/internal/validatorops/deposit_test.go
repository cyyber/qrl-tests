package validatorops

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/consensuscrypto"
	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/accounts/abi"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
)

func TestDepositEventAmount(t *testing.T) {
	parsed, err := abi.JSON(strings.NewReader(depositContractABI))
	require.NoError(t, err)
	address := common.Address{1}
	depositor := &Depositor{
		address:  address,
		contract: bind.NewBoundContract(address, parsed, nil, nil, nil),
	}
	data := consensuscrypto.DepositData{
		PublicKey:           bytes.Repeat([]byte{0x11}, consensuscrypto.PublicKeyLength),
		WithdrawalRecipient: bytes.Repeat([]byte{0x22}, consensuscrypto.WithdrawalRecipientLength),
		RandaoCommitment:    bytes.Repeat([]byte{0x33}, consensuscrypto.RandaoCommitmentLength),
		Signature:           bytes.Repeat([]byte{0x44}, consensuscrypto.SignatureLength),
		Amount:              20000000000000,
	}
	for _, test := range []struct {
		name      string
		amount    []byte
		wantError string
	}{
		{"matching", binary.LittleEndian.AppendUint64(nil, data.Amount), ""},
		{"undercredited", binary.LittleEndian.AppendUint64(nil, data.Amount-1), "deposit event amount is"},
		{"overcredited", binary.LittleEndian.AppendUint64(nil, data.Amount+1), "deposit event amount is"},
		{"empty", nil, "deposit event amount must be 8 bytes"},
		{"short", make([]byte, 7), "deposit event amount must be 8 bytes"},
		{"long", make([]byte, 9), "deposit event amount must be 8 bytes"},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := parsed.Events["DepositEvent"].Inputs.Pack(
				data.PublicKey, data.WithdrawalRecipient, test.amount,
				data.RandaoCommitment, data.Signature, make([]byte, 8),
			)
			require.NoError(t, err)
			receipt := &types.Receipt{Logs: []*types.Log{{
				Address: address,
				Topics:  []common.LogTopic{common.HashToLogTopic(parsed.Events["DepositEvent"].ID)},
				Data:    encoded,
			}}}
			err = depositor.verifyEvent(receipt, data)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
