package console

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/theQRL/go-qrl/accounts/abi"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
)

type deploymentParameters struct {
	Sender         string          `json:"sender"`
	TxHash         string          `json:"txHash"`
	RawTransaction string          `json:"rawTransaction"`
	ABI            json.RawMessage `json:"abi"`
}

type preparedDeployment struct {
	auth *bind.TransactOpts
	tx   *types.Transaction
	raw  []byte
}

func prepareContractParameters(
	ctx context.Context,
	node *live.Node,
	abiJSON, bytecode []byte,
) ([]byte, error) {
	deployment, err := prepareDeploymentTransaction(ctx, node, abiJSON, bytecode)
	if err != nil {
		return nil, err
	}
	return json.Marshal(deploymentParameters{
		Sender:         deployment.auth.From.Hex(),
		TxHash:         deployment.tx.Hash().Hex(),
		RawTransaction: hexutil.Encode(deployment.raw),
		ABI:            abiJSON,
	})
}

// prepareDeploymentTransaction builds and signs the creation transaction.
// The console scenario broadcasts its raw encoding.
func prepareDeploymentTransaction(
	ctx context.Context,
	node *live.Node,
	abiJSON, bytecode []byte,
) (preparedDeployment, error) {
	contractABI, err := abi.JSON(bytes.NewReader(abiJSON))
	if err != nil {
		return preparedDeployment{}, fmt.Errorf("parse contract ABI: %w", err)
	}
	auth, err := newTransactor(ctx, node)
	if err != nil {
		return preparedDeployment{}, err
	}

	_, tx, _, err := bind.DeployContract(auth, contractABI, bytecode, node.Execution)
	if err != nil {
		return preparedDeployment{}, fmt.Errorf("prepare deployment transaction: %w", err)
	}
	raw, err := tx.MarshalBinary()
	if err != nil {
		return preparedDeployment{}, fmt.Errorf("encode deployment transaction: %w", err)
	}
	return preparedDeployment{auth: auth, tx: tx, raw: raw}, nil
}

func newTransactor(ctx context.Context, node *live.Node) (*bind.TransactOpts, error) {
	auth, err := bind.NewKeyedTransactorWithChainID(node.Wallet, node.ChainID)
	if err != nil {
		return nil, fmt.Errorf("create console transactor: %w", err)
	}
	auth.Context = ctx
	auth.NoSend = true
	return auth, nil
}
