package fixture

import (
	_ "embed"
	"fmt"
	"strings"

	qrlwallet "github.com/theQRL/go-qrl/crypto/pqcrypto/wallet"
)

//go:embed testdata/unsafe-development-wallet.seed
var developmentWalletSeed string

const DevelopmentWalletAddress = "QBb81a0496aa34a64f96c2bCd28793165e1e6C08af0605b119cc768764901d2E4B48b5b9c049C57469CcA8a0421D2E31DF5C637a9cee8f3DA83964261B6CF9a22"

func DevelopmentWallet() (qrlwallet.Wallet, error) {
	wallet, err := qrlwallet.RestoreFromSeedHex(strings.TrimSpace(developmentWalletSeed))
	if err != nil {
		return nil, fmt.Errorf("restore development wallet: %w", err)
	}
	return wallet, nil
}
