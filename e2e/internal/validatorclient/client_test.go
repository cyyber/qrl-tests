package validatorclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeymanagerAPI(t *testing.T) {
	const token = "0x" + "aa"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer "+token, request.Header.Get("Authorization"))
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/qrl/v1/keystores":
			_, _ = writer.Write([]byte(`{"data":[{"validating_pubkey":"0xab"}]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/qrl/v1/keystores":
			var body struct {
				Keystores []string `json:"keystores"`
				Passwords []string `json:"passwords"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			require.Equal(t, []string{`{"pubkey":"ab"}`}, body.Keystores)
			require.Equal(t, []string{"secret"}, body.Passwords)
			_, _ = writer.Write([]byte(`{"data":[{"status":"imported"}]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/qrl/v1/validator/0xab/voluntary_exit":
			require.Equal(t, "3", request.URL.Query().Get("epoch"))
			_, _ = writer.Write([]byte(`{"data":{"message":{"epoch":"3","validator_index":"64"},"signature":"0xcd"}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, token)
	require.NoError(t, err)

	keystores, err := client.ListKeystores(t.Context())
	require.NoError(t, err)
	require.Equal(t, []Keystore{{PublicKey: "0xab"}}, keystores)

	require.NoError(t, client.ImportKeystore(t.Context(), `{"pubkey":"ab"}`, "secret"))

	exit, err := client.SignVoluntaryExit(t.Context(), "0xab", 3)
	require.NoError(t, err)
	require.Equal(t, uint64(3), exit.Message.Epoch)
	require.Equal(t, uint64(64), exit.Message.ValidatorIndex)
	require.Equal(t, "0xcd", exit.Signature)
}

func TestNewRequiresToken(t *testing.T) {
	_, err := New("http://validator.test", "")
	require.ErrorContains(t, err, "token")
}
