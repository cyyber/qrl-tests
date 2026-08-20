//go:build e2e

package console

import (
	"context"
	"fmt"
	"math/big"

	endtoendlive "github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/theQRL/go-qrl/accounts/abi"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/params"
)

// clefAccountPool is the number of development accounts the pinned
// qrl-package imports into clef; bump it together with PackageLocator.
const clefAccountPool = 10

func fundManagedAccount(ctx context.Context, session *endtoendlive.Node) error {
	var managed []common.Address
	if err := session.Execution.Client().CallContext(ctx, &managed, "qrl_accounts"); err != nil {
		return fmt.Errorf("list node-managed accounts: %w", err)
	}
	if len(managed) != clefAccountPool {
		return fmt.Errorf("expected the clef account pool of %d, got %d", clefAccountPool, len(managed))
	}

	auth, err := bind.NewKeyedTransactorWithChainID(session.Wallet, session.ChainID)
	if err != nil {
		return fmt.Errorf("create funding transactor: %w", err)
	}
	auth.Context = ctx
	auth.GasLimit = params.TxGas
	auth.Value = new(big.Int).Mul(big.NewInt(10), big.NewInt(params.Quanta))
	funding := bind.NewBoundContract(managed[0], abi.ABI{}, session.Execution, session.Execution, session.Execution)
	tx, err := funding.Transfer(auth)
	if err != nil {
		return fmt.Errorf("fund node-managed account: %w", err)
	}
	receipt, err := bind.WaitMined(ctx, session.Execution, tx)
	if err != nil {
		return fmt.Errorf("wait for node-managed account funding: %w", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return fmt.Errorf("node-managed account funding transaction %s failed", tx.Hash())
	}
	return nil
}
