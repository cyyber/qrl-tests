package remotesigner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/rpc"
)

const executionAddress = "Q" + "52" + "53000000000000000000000000000000000000000000000000000000000000" +
	"0000000000000000000000000000000000000000000000000000000000000000"

func TestClientListsTheExecutionAccounts(t *testing.T) {
	key := "0x" + strings.Repeat("cd", 2592)
	signer := newSigner(t, map[string]http.HandlerFunc{
		"GET /api/v1/execution/publicKeys": func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, `[{"address":"`+executionAddress+`","public_key":"`+key+`"}]`)
		},
	})
	accounts, err := signer.client(t, testToken).ExecutionAccounts(t.Context())
	require.NoError(t, err)
	require.Equal(t, []ExecutionAccount{{Address: executionAddress, PublicKey: key}}, accounts)
	require.Equal(t, "Bearer "+testToken, signer.requests[0].Header.Get("Authorization"))

	// A field the contract does not have is refused.
	signer = newSigner(t, map[string]http.HandlerFunc{
		"GET /api/v1/execution/publicKeys": func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, `[{"address":"`+executionAddress+`","public_key":"0x","seed":"0x"}]`)
		},
	})
	_, err = signer.client(t, testToken).ExecutionAccounts(t.Context())
	require.ErrorContains(t, err, "unknown field")
}

func TestClientDialsTheJSONRPCEndpoint(t *testing.T) {
	signer := newSigner(t, map[string]http.HandlerFunc{
		"POST /": func(writer http.ResponseWriter, request *http.Request) {
			var call struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			if request.Header.Get("Authorization") != "Bearer "+testToken {
				writer.WriteHeader(http.StatusUnauthorized)
				return
			}
			if call.Method == "admin_nodeInfo" {
				_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":`+string(call.ID)+
					`,"error":{"code":-32601,"message":"Method not found"}}`)
				return
			}
			_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":`+string(call.ID)+`,"result":["`+executionAddress+`"]}`)
		},
	})

	client, err := signer.client(t, testToken).DialRPC(t.Context())
	require.NoError(t, err)
	t.Cleanup(client.Close)
	var accounts []string
	require.NoError(t, client.CallContext(t.Context(), &accounts, "qrl_accounts"))
	require.Equal(t, []string{executionAddress}, accounts)
	require.Equal(t, "/", signer.requests[0].URL.Path)

	err = client.CallContext(t.Context(), nil, "admin_nodeInfo")
	var rpcErr rpc.Error
	require.ErrorAs(t, err, &rpcErr)
	require.Equal(t, CodeMethodNotFound, rpcErr.ErrorCode())
	require.Equal(t, MessageMethodNotFound, rpcErr.Error())

	// Without the token the signer answers 401, before any JSON-RPC.
	anonymous, err := signer.client(t, testToken).WithToken("").DialRPC(t.Context())
	require.NoError(t, err)
	t.Cleanup(anonymous.Close)
	err = anonymous.CallContext(t.Context(), &accounts, "qrl_accounts")
	var httpErr rpc.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusUnauthorized, httpErr.StatusCode)
	require.Empty(t, signer.requests[len(signer.requests)-1].Header.Get("Authorization"))
}

func TestParseAuditLog(t *testing.T) {
	log := `{"time":"2026-10-20T08:15:02.731Z","method":"qrl_sendTransaction","outcome":"signed","hash":"0x5bfe","from":"Q45f3","to":"Q1111","nonce":"0x7","value":"0xde0b6b3a7640000","fee":"0xeebe0b40ed208","chainId":"0x539"}
{"time":"2026-10-20T08:15:02.801Z","method":"qrl_sendTransaction","outcome":"sent","hash":"0x5bfe","from":"Q45f3","to":"Q1111","nonce":"0x7","value":"0xde0b6b3a7640000","fee":"0xeebe0b40ed208","chainId":"0x539"}
{"time":"2026-10-20T08:16:00Z","method":"qrl_signTransaction","outcome":"refused","from":"Q45f3","reason":"chainId 1 is not --chain-id 1337"}
`
	entries, err := ParseAuditLog([]byte(log))
	require.NoError(t, err)
	require.Len(t, entries, 3)
	require.Equal(t, AuditEntry{
		Time: "2026-10-20T08:15:02.731Z", Method: "qrl_sendTransaction", Outcome: AuditSigned, Hash: "0x5bfe",
		From: "Q45f3", To: "Q1111", Nonce: "0x7", Value: "0xde0b6b3a7640000", Fee: "0xeebe0b40ed208", ChainID: "0x539",
	}, entries[0])
	require.Equal(t, AuditSent, entries[1].Outcome)
	require.Equal(t, AuditRefused, entries[2].Outcome)
	require.Equal(t, "chainId 1 is not --chain-id 1337", entries[2].Reason)

	entries, err = ParseAuditLog(nil)
	require.NoError(t, err)
	require.Empty(t, entries)

	_, err = ParseAuditLog([]byte(`{"time":"t","method":"m","outcome":"signed"}` + "\n" + `{"outcome":"sent","gas":"0x1"}` + "\n"))
	require.ErrorContains(t, err, "audit log line 2")
}

func TestWriteExecutionDiagnostics(t *testing.T) {
	signer := newSigner(t, map[string]http.HandlerFunc{
		"GET /healthcheck": func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, `{"status":"UP","outcome":"UP"}`)
		},
	})
	metrics := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, metricsFixture)
	}))
	t.Cleanup(metrics.Close)
	const log = `{"time":"t","method":"qrl_sendTransaction","outcome":"sent"}` + "\n"

	dir := filepath.Join(t.TempDir(), "execution-signer")
	err := WriteExecutionDiagnostics(t.Context(), dir, signer.client(t, testToken), metrics.URL,
		func(context.Context) ([]byte, error) { return []byte(log), nil })
	require.NoError(t, err)
	read := func(name string) string {
		body, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		return string(body)
	}
	require.Equal(t, metricsFixture, read(DiagnosticsMetricsFile))
	require.JSONEq(t, `{"status":"UP","outcome":"UP"}`, read(DiagnosticsHealthcheckFile))
	require.Equal(t, log, read(DiagnosticsAuditLogFile))
	_, err = os.Stat(filepath.Join(dir, DiagnosticsCapabilitiesFile))
	require.ErrorIs(t, err, os.ErrNotExist, "execution mode has no capabilities")

	dir = t.TempDir()
	err = WriteExecutionDiagnostics(t.Context(), dir, signer.client(t, testToken), metrics.URL,
		func(context.Context) ([]byte, error) { return nil, errors.New("no such container") })
	require.EqualError(t, err, "audit.jsonl: no such container")
	require.Equal(t, "audit.jsonl: no such container\n", read(DiagnosticsErrorsFile))
}
