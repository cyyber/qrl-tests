package remotesigner

import (
	"bufio"
	"bytes"
	"context"
	"fmt"

	"github.com/theQRL/go-qrl/rpc"
)

// Execution mode's routes and metrics, of go-remotesigner's
// docs/operations/execution.md: the JSON-RPC endpoint at POST / and the
// account list.
const (
	executionPublicKeysPath = "/api/v1/execution/publicKeys"

	// ExecutionRPCRoute is the route label of MetricHTTPRequestDurations
	// for the JSON-RPC endpoint, whose pattern matches the path / alone.
	ExecutionRPCRoute = "/{$}"
	// MetricSignersLoaded is the number of loaded keys, in execution mode
	// the accounts.
	MetricSignersLoaded = "signing_signers_loaded_count"
)

// Codes and messages of the JSON-RPC errors the signer itself answers with.
const (
	// CodeMethodNotFound is a method the signer neither answers nor
	// forwards; MessageMethodNotFound is the signer's own message for it,
	// which gqrl's differs from.
	CodeMethodNotFound    = -32601
	MessageMethodNotFound = "Method not found"
	// CodeTransactionRejected is a transaction the signer refuses to sign:
	// its chain ID, its fee or the account's policy. The error's data says
	// why.
	CodeTransactionRejected = -32003
)

// ExecutionAccount is an account the signer holds: its address, Q and 128
// hex characters in QIP-55 form, and its 2,592-byte ML-DSA-87 public key in
// 0x hex.
type ExecutionAccount struct {
	Address   string `json:"address"`
	PublicKey string `json:"public_key"`
}

// ExecutionAccounts lists the accounts, sorted by address.
func (client *Client) ExecutionAccounts(ctx context.Context) ([]ExecutionAccount, error) {
	body, err := client.get(ctx, executionPublicKeysPath)
	if err != nil {
		return nil, err
	}
	var accounts []ExecutionAccount
	if err := decodeStrict(body, &accounts); err != nil {
		return nil, fmt.Errorf("decode %s: %w", executionPublicKeysPath, err)
	}
	return accounts, nil
}

// DialRPC returns a JSON-RPC client of the endpoint at POST /, over the
// client's TLS configuration and with its token, which the RPC client sends
// as a bearer token only.
func (client *Client) DialRPC(ctx context.Context) (*rpc.Client, error) {
	options := []rpc.ClientOption{rpc.WithHTTPClient(client.http)}
	if client.token != "" {
		options = append(options, rpc.WithHeader("Authorization", "Bearer "+client.token))
	}
	return rpc.DialOptions(ctx, client.baseURL.JoinPath("/").String(), options...)
}

// Outcomes of AuditEntry.
const (
	// AuditSigned is a signed transaction, before it leaves the signer.
	AuditSigned = "signed"
	// AuditSent is a sent transaction the execution client accepted.
	AuditSent = "sent"
	// AuditNodeRefused is a sent transaction the execution client refused.
	AuditNodeRefused = "node-refused"
	// AuditRefused is a transaction the signer refused to sign.
	AuditRefused = "refused"
)

// AuditEntry is a line of the audit log of execution mode,
// --execution-audit-log-file. Quantities are 0x hex; the fields the signer
// had no value for are empty.
type AuditEntry struct {
	Time    string `json:"time"`
	Method  string `json:"method"`
	Outcome string `json:"outcome"`
	Hash    string `json:"hash,omitempty"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Nonce   string `json:"nonce,omitempty"`
	Value   string `json:"value,omitempty"`
	// Fee is gas × maxFeePerGas.
	Fee     string `json:"fee,omitempty"`
	ChainID string `json:"chainId,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// ParseAuditLog decodes the audit log, one JSON object per line, strictly:
// a field the log gains is a change the suite must see.
func ParseAuditLog(log []byte) ([]AuditEntry, error) {
	var entries []AuditEntry
	scanner := bufio.NewScanner(bytes.NewReader(log))
	scanner.Buffer(nil, maxBodyBytes)
	for line := 1; scanner.Scan(); line++ {
		var entry AuditEntry
		if err := decodeStrict(scanner.Bytes(), &entry); err != nil {
			return nil, fmt.Errorf("audit log line %d: %w", line, err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read the audit log: %w", err)
	}
	return entries, nil
}
