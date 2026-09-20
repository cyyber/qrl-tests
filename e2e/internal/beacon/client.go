// Package beacon implements a minimal client for the beacon node REST API.
package beacon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const requestTimeout = 10 * time.Second

type Client struct {
	baseURL *url.URL
	http    *http.Client
}

type responseError struct {
	method     string
	path       string
	status     string
	statusCode int
	body       string
}

func (err *responseError) Error() string {
	return fmt.Sprintf("%s %s returned %s: %s", err.method, err.path, err.status, err.body)
}

// IsNotFound reports whether err is a 404 from the beacon API, which is how a
// missed slot or an unknown validator is reported.
func IsNotFound(err error) bool {
	var responseErr *responseError
	return errors.As(err, &responseErr) && responseErr.statusCode == http.StatusNotFound
}

func New(endpoint string) (*Client, error) {
	baseURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse beacon endpoint: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("beacon endpoint %q must be an absolute URL", endpoint)
	}
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: requestTimeout}}, nil
}

type dataResponse[T any] struct {
	Data T `json:"data"`
}

func getData[T any](ctx context.Context, client *Client, path string) (T, error) {
	var response dataResponse[T]
	if err := client.do(ctx, http.MethodGet, path, nil, &response); err != nil {
		var zero T
		return zero, err
	}
	return response.Data, nil
}

func postData[T any](ctx context.Context, client *Client, path string, payload any) (T, error) {
	var response dataResponse[T]
	if err := client.postJSON(ctx, path, payload, &response); err != nil {
		var zero T
		return zero, err
	}
	return response.Data, nil
}

func (client *Client) postJSON(ctx context.Context, path string, payload, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return client.do(ctx, http.MethodPost, path, bytes.NewReader(body), result)
}

func (client *Client) do(ctx context.Context, method, path string, body io.Reader, result any) error {
	reference, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("parse beacon path %q: %w", path, err)
	}
	endpoint := client.baseURL.ResolveReference(reference)

	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return &responseError{
			method: method, path: path, status: response.Status,
			statusCode: response.StatusCode, body: strings.TrimSpace(string(payload)),
		}
	}
	if result == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(result); err != nil {
		return fmt.Errorf("decode %s %s: %w", method, path, err)
	}
	return nil
}
