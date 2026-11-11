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

type keystoreStatus struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// ImportKeystore imports one keystore. A keystore the validator already holds
// is reported as a duplicate, which counts as imported.
func (client *Client) ImportKeystore(ctx context.Context, keystoreJSON, password string) error {
	return client.ImportKeystoreWithHistory(ctx, keystoreJSON, password, "")
}

// ImportKeystoreWithHistory imports one keystore with slashingProtection, its
// EIP-3076 history, which the validator client imports first. A history the
// client rejects fails the import.
func (client *Client) ImportKeystoreWithHistory(ctx context.Context, keystoreJSON, password, slashingProtection string) error {
	var response dataResponse[[]keystoreStatus]
	if err := client.do(ctx, http.MethodPost, "/qrl/v1/keystores", struct {
		Keystores          []string `json:"keystores"`
		Passwords          []string `json:"passwords"`
		SlashingProtection string   `json:"slashing_protection,omitempty"`
	}{
		Keystores:          []string{keystoreJSON},
		Passwords:          []string{password},
		SlashingProtection: slashingProtection,
	}, &response); err != nil {
		return err
	}
	return checkStatus("import keystore", response.Data, "imported", "duplicate")
}

// DeleteKeystore deletes the keystore of publicKey and returns the EIP-3076
// history the validator client exports for that key.
func (client *Client) DeleteKeystore(ctx context.Context, publicKey string) (string, error) {
	var response struct {
		Data               []keystoreStatus `json:"data"`
		SlashingProtection string           `json:"slashing_protection"`
	}
	if err := client.do(ctx, http.MethodDelete, "/qrl/v1/keystores", struct {
		PublicKeys []string `json:"pubkeys"`
	}{
		PublicKeys: []string{publicKey},
	}, &response); err != nil {
		return "", err
	}
	if err := checkStatus("delete keystore", response.Data, "deleted"); err != nil {
		return "", err
	}
	return response.SlashingProtection, nil
}

// checkStatus checks the one status of a single-keystore request against the
// accepted ones, ignoring case.
func checkStatus(action string, statuses []keystoreStatus, accepted ...string) error {
	if len(statuses) != 1 {
		return fmt.Errorf("%s: expected 1 status, got %d", action, len(statuses))
	}
	result := statuses[0]
	for _, status := range accepted {
		if strings.EqualFold(result.Status, status) {
			return nil
		}
	}
	if result.Message != "" {
		return fmt.Errorf("%s: %s: %s", action, result.Status, result.Message)
	}
	return fmt.Errorf("%s: %s", action, result.Status)
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
