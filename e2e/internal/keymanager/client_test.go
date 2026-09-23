package keymanager

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testToken = "0x" + "aa"

// newTestClient serves handler behind the bearer-token check. The handler runs
// on the server goroutine, so it must use assert rather than require: the test
// still fails, but FailNow is only valid on the test goroutine.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "Bearer "+testToken, request.Header.Get("Authorization"))
		handler(writer, request)
	}))
	t.Cleanup(server.Close)

	client, err := New(server.URL, testToken)
	require.NoError(t, err)
	return client
}

func TestClientDecodesQrysmResponses(t *testing.T) {
	client := newTestClient(t, func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/qrl/v1/keystores":
			_, _ = writer.Write([]byte(`{"data":[{"validating_pubkey":"0xab","derivation_path":"","readonly":false}]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/qrl/v1/keystores":
			assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
			var body struct {
				Keystores []string `json:"keystores"`
				Passwords []string `json:"passwords"`
			}
			assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			assert.Equal(t, []string{`{"pubkey":"ab"}`}, body.Keystores)
			assert.Equal(t, []string{"secret"}, body.Passwords)
			_, _ = writer.Write([]byte(`{"data":[{"status":"imported"}]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/qrl/v1/validator/0xab/voluntary_exit":
			assert.Equal(t, "3", request.URL.Query().Get("epoch"))
			_, _ = writer.Write([]byte(`{"data":{"message":{"epoch":"3","validator_index":"64"},"signature":"0xcd"}}`))
		default:
			http.NotFound(writer, request)
		}
	})

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

func TestImportKeystoreStatus(t *testing.T) {
	for _, test := range []struct {
		name     string
		response string
		wantErr  string
	}{
		{name: "imported", response: `{"data":[{"status":"imported"}]}`},
		{name: "duplicate", response: `{"data":[{"status":"DUPLICATE"}]}`},
		{name: "error with message", response: `{"data":[{"status":"error","message":"bad password"}]}`, wantErr: "import keystore: error: bad password"},
		{name: "error without message", response: `{"data":[{"status":"error"}]}`, wantErr: "import keystore: error"},
		{name: "no status", response: `{"data":[]}`, wantErr: "import keystore: expected 1 status, got 0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newTestClient(t, func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(test.response))
			})
			err := client.ImportKeystore(t.Context(), `{}`, "secret")
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, test.wantErr)
		})
	}
}

func TestClientReportsErrorResponses(t *testing.T) {
	client := newTestClient(t, func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, `{"message":"unauthorized"}`, http.StatusUnauthorized)
	})

	_, err := client.ListKeystores(t.Context())
	require.EqualError(t, err, `GET /qrl/v1/keystores returned 401 Unauthorized: {"message":"unauthorized"}`)
}

func TestNewValidatesArguments(t *testing.T) {
	_, err := New("validator.test", testToken)
	require.ErrorContains(t, err, "absolute URL")

	_, err = New("http://validator.test", "")
	require.ErrorContains(t, err, "token")
}
