package validatorops

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/common/hexutil"
)

func TestGenesisKeyMatchesDepositCLI(t *testing.T) {
	// The public keys' first bytes in the keystores qrysm's deposit CLI wrote
	// for the default mnemonic's validators 0 and 1.
	for index, prefix := range []string{"c109135d398510b96f993b3ed4f3c446", "efe2b8e33d7098c7dc695fb8ba499c8d"} {
		key, err := GenesisKey(devnet.GenesisMnemonic, uint64(index))
		require.NoError(t, err)
		require.Equal(t, prefix, hex.EncodeToString(key.PublicKey()[:16]), "validator %d", index)
	}
	_, err := GenesisKey("not a mnemonic", 0)
	require.ErrorContains(t, err, "decode mnemonic")
}

func TestGenesisKeysChecksTheBeaconState(t *testing.T) {
	first, err := GenesisKey(devnet.GenesisMnemonic, 3)
	require.NoError(t, err)
	second, err := GenesisKey(devnet.GenesisMnemonic, 9)
	require.NoError(t, err)
	record := func(index uint64, key *Key, status string) string {
		return fmt.Sprintf(`{"index":"%d","status":"%s","validator":{"pubkey":"%s"}}`, index, status, hexutil.Encode(key.PublicKey()))
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var records string
		switch request.URL.Query().Get("id") {
		case "3":
			records = record(3, first, "active_ongoing") + "," + record(9, second, "active_ongoing")
		case "4":
			records = record(4, first, "active_ongoing")
		case "9":
			records = record(9, second, "active_exiting")
		}
		_, _ = fmt.Fprintf(writer, `{"data":[%s]}`, records)
	}))
	defer server.Close()
	client, err := beacon.New(server.URL)
	require.NoError(t, err)

	keys, err := GenesisKeys(t.Context(), client, []uint64{3, 9})
	require.NoError(t, err)
	require.Equal(t, first.PublicKey(), keys[0].PublicKey())
	require.Equal(t, second.PublicKey(), keys[1].PublicKey())

	_, err = GenesisKeys(t.Context(), client, []uint64{4})
	require.ErrorContains(t, err, "genesis validator 4 is not the key derived for its index")
	_, err = GenesisKeys(t.Context(), client, []uint64{9})
	require.ErrorContains(t, err, "genesis validator 9 is active_exiting")
}
