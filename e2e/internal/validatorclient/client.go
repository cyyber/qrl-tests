// Package validatorclient is the minimal Qrysm validator keymanager client
// the consensus suites use.
package validatorclient

import (
	"bytes"
	"context"
	"encoding/json"
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
		return nil, fmt.Errorf("parse validator endpoint: %w", err)
	}
	if token == "" {
		return nil, fmt.Errorf("validator keymanager token is required")
	}
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: requestTimeout},
	}, nil
}

type Keystore struct {
	PublicKey string
}

func (client *Client) ListKeystores(ctx context.Context) ([]Keystore, error) {
	var response struct {
		Data []struct {
			PublicKey string `json:"validating_pubkey"`
		} `json:"data"`
	}
	if err := client.do(ctx, http.MethodGet, "/qrl/v1/keystores", nil, &response); err != nil {
		return nil, err
	}
	keystores := make([]Keystore, len(response.Data))
	for index, item := range response.Data {
		keystores[index] = Keystore{PublicKey: item.PublicKey}
	}
	return keystores, nil
}

func (client *Client) ImportKeystore(ctx context.Context, keystoreJSON, password string) error {
	var response struct {
		Data []struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := client.do(ctx, http.MethodPost, "/qrl/v1/keystores", map[string]any{
		"keystores": []string{keystoreJSON},
		"passwords": []string{password},
	}, &response); err != nil {
		return err
	}
	if len(response.Data) != 1 {
		return fmt.Errorf("import keystore: expected 1 status, got %d", len(response.Data))
	}
	status := strings.ToLower(response.Data[0].Status)
	if status == "imported" || status == "duplicate" {
		return nil
	}
	if response.Data[0].Message != "" {
		return fmt.Errorf("import keystore: %s: %s", response.Data[0].Status, response.Data[0].Message)
	}
	return fmt.Errorf("import keystore: %s", response.Data[0].Status)
}

func (client *Client) SignVoluntaryExit(ctx context.Context, publicKey string, epoch uint64) (beacon.SignedVoluntaryExit, error) {
	path := "/qrl/v1/validator/" + url.PathEscape(publicKey) + "/voluntary_exit?epoch=" + strconv.FormatUint(epoch, 10)
	var response struct {
		Data beacon.SignedVoluntaryExit `json:"data"`
	}
	if err := client.do(ctx, http.MethodPost, path, struct{}{}, &response); err != nil {
		return beacon.SignedVoluntaryExit{}, err
	}
	return response.Data, nil
}

func (client *Client) do(ctx context.Context, method, path string, payload, result any) error {
	reference, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("parse validator path %q: %w", path, err)
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
