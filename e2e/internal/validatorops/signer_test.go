package validatorops

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/signing"
	"github.com/stretchr/testify/require"
)

func TestSignerSignsUnderTheChainsDomain(t *testing.T) {
	genesisValidatorsRoot := "0x" + strings.Repeat("77", signing.RootLength)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/qrl/v1/beacon/genesis":
			_, _ = fmt.Fprintf(writer, `{"data":{"genesis_time":"1700000000","genesis_validators_root":%q,"genesis_fork_version":"0x10000020"}}`,
				genesisValidatorsRoot)
		case "/qrl/v1/beacon/states/head/fork":
			_, _ = writer.Write([]byte(`{"data":{"previous_version":"0x10000020","current_version":"0x10000021","epoch":"5"}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	client, err := beacon.New(server.URL)
	require.NoError(t, err)
	key, err := DeterministicKey(0x91)
	require.NoError(t, err)

	signer, err := LoadSigner(t.Context(), client, key)
	require.NoError(t, err)
	root, err := signing.ParseRoot(genesisValidatorsRoot)
	require.NoError(t, err)
	require.Equal(t, root, signer.GenesisValidatorsRoot)
	require.Equal(t, signing.Fork{
		PreviousVersion: signing.ForkVersion{0x10, 0x00, 0x00, 0x20},
		CurrentVersion:  signing.ForkVersion{0x10, 0x00, 0x00, 0x21},
		Epoch:           5,
	}, signer.Fork)

	// Epoch 4 precedes the fork, so it signs with the previous version.
	object := signing.Root{0x01}
	signature, err := signer.Sign(signing.DomainBeaconProposer, 4, object)
	require.NoError(t, err)
	decoded, err := hex.DecodeString(strings.TrimPrefix(signature, "0x"))
	require.NoError(t, err)
	previous := signing.ComputeDomain(signing.DomainBeaconProposer, signer.Fork.PreviousVersion, root)
	require.NoError(t, signing.Verify(signing.SigningRoot(object, previous), key.PublicKey(), decoded))
	current := signing.ComputeDomain(signing.DomainBeaconProposer, signer.Fork.CurrentVersion, root)
	require.Error(t, signing.Verify(signing.SigningRoot(object, current), key.PublicKey(), decoded))
}
