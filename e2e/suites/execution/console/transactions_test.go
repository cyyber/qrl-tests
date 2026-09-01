package console

import (
	"bytes"
	"encoding/json"

	"github.com/theQRL/go-qrl/common"
)

type transactionParameters struct {
	TransferRecipient string `json:"transferRecipient"`
}

func prepareTransactionParameters() ([]byte, error) {
	recipient := common.BytesToAddress(bytes.Repeat([]byte{0xa5}, common.AddressLength))
	return json.Marshal(transactionParameters{TransferRecipient: recipient.Hex()})
}
