// Package keymanager is a minimal Qrysm validator keymanager REST client.
package keymanager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
)

const requestTimeout = 10 * time.Second

type Client struct {
	baseURL *url.URL
	token   string
	http    *http.Client
}

func New(endpoint, token string) (*Client, error) {
	baseURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse keymanager endpoint: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("keymanager endpoint %q must be an absolute URL", endpoint)
	}
	if token == "" {
		return nil, errors.New("keymanager token is required")
	}
	return &Client{baseURL: baseURL, token: token, http: &http.Client{Timeout: requestTimeout}}, nil
}

type dataResponse[T any] struct {
	Data T `json:"data"`
}

type Keystore struct {
	PublicKey string `json:"validating_pubkey"`
}

// ContainsPublicKey reports whether keystores holds publicKey, ignoring case
// and the 0x prefix.
func ContainsPublicKey(keystores []Keystore, publicKey string) bool {
	wanted := strings.TrimPrefix(strings.ToLower(publicKey), "0x")
	for _, keystore := range keystores {
		if strings.TrimPrefix(strings.ToLower(keystore.PublicKey), "0x") == wanted {
			return true
		}
	}
	return false
}

func (client *Client) ListKeystores(ctx context.Context) ([]Keystore, error) {
	var response dataResponse[[]Keystore]
	if err := client.do(ctx, http.MethodGet, "/qrl/v1/keystores", nil, &response); err != nil {
		return nil, err
	}
	return response.Data, nil
}

type importStatus struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// ImportKeystore imports one keystore. A keystore the validator already holds
// is reported as a duplicate, which counts as imported.
func (client *Client) ImportKeystore(ctx context.Context, keystoreJSON, password string) error {
	var response dataResponse[[]importStatus]
	if err := client.do(ctx, http.MethodPost, "/qrl/v1/keystores", struct {
		Keystores []string `json:"keystores"`
		Passwords []string `json:"passwords"`
	}{
		Keystores: []string{keystoreJSON},
		Passwords: []string{password},
	}, &response); err != nil {
		return err
	}
	if len(response.Data) != 1 {
		return fmt.Errorf("import keystore: expected 1 status, got %d", len(response.Data))
	}
	result := response.Data[0]
	if strings.EqualFold(result.Status, "imported") || strings.EqualFold(result.Status, "duplicate") {
		return nil
	}
	if result.Message != "" {
		return fmt.Errorf("import keystore: %s: %s", result.Status, result.Message)
	}
	return fmt.Errorf("import keystore: %s", result.Status)
}

// RemoteKey is a key of a validator client that signs through a remote
// signer, as its remote keys API lists it. URL is the signer the key is
// signed by; ReadOnly is false for the keys of a key file, the only ones the
// API can add and delete.
type RemoteKey struct {
	PublicKey string `json:"pubkey"`
	URL       string `json:"url"`
	ReadOnly  bool   `json:"readonly"`
}

// Status is the API's answer for one key of a request, such as imported,
// duplicate or error for an import, and deleted, not_found or error for a
// delete; qrysm's case varies, so compare with strings.EqualFold.
type Status struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

const remoteKeysPath = "/qrl/v1/remotekeys"

func (client *Client) ListRemoteKeys(ctx context.Context) ([]RemoteKey, error) {
	var response dataResponse[[]RemoteKey]
	if err := client.do(ctx, http.MethodGet, remoteKeysPath, nil, &response); err != nil {
		return nil, err
	}
	return response.Data, nil
}

// ImportRemoteKey adds publicKey to the validator client's keys, giving
// signerURL as the key's signer, and returns the key's status.
func (client *Client) ImportRemoteKey(ctx context.Context, publicKey, signerURL string) (Status, error) {
	type remoteKey struct {
		PublicKey string `json:"pubkey"`
		URL       string `json:"url"`
	}
	return client.oneStatus(ctx, http.MethodPost, struct {
		RemoteKeys []remoteKey `json:"remote_keys"`
	}{RemoteKeys: []remoteKey{{PublicKey: publicKey, URL: signerURL}}})
}

// DeleteRemoteKey removes publicKey from the validator client's keys and
// returns the key's status.
func (client *Client) DeleteRemoteKey(ctx context.Context, publicKey string) (Status, error) {
	return client.oneStatus(ctx, http.MethodDelete, struct {
		PublicKeys []string `json:"pubkeys"`
	}{PublicKeys: []string{publicKey}})
}

func (client *Client) oneStatus(ctx context.Context, method string, payload any) (Status, error) {
	var response dataResponse[[]Status]
	if err := client.do(ctx, method, remoteKeysPath, payload, &response); err != nil {
		return Status{}, err
	}
	if len(response.Data) != 1 {
		return Status{}, fmt.Errorf("%s %s: expected 1 status, got %d", method, remoteKeysPath, len(response.Data))
	}
	return response.Data[0], nil
}

func (client *Client) SignVoluntaryExit(ctx context.Context, publicKey string, epoch uint64) (beacon.SignedVoluntaryExit, error) {
	path := "/qrl/v1/validator/" + url.PathEscape(publicKey) + "/voluntary_exit?epoch=" + strconv.FormatUint(epoch, 10)
	var response dataResponse[beacon.SignedVoluntaryExit]
	if err := client.do(ctx, http.MethodPost, path, struct{}{}, &response); err != nil {
		return beacon.SignedVoluntaryExit{}, err
	}
	return response.Data, nil
}

func (client *Client) do(ctx context.Context, method, path string, payload, result any) error {
	reference, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("parse keymanager path %q: %w", path, err)
	}

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, client.baseURL.ResolveReference(reference).String(), body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("%s %s returned %s: %s", method, path, response.Status, strings.TrimSpace(string(message)))
	}
	if result == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(result); err != nil {
		return fmt.Errorf("decode %s %s: %w", method, path, err)
	}
	return nil
}
