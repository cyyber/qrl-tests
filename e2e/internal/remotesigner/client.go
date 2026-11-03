// Package remotesigner is a client for go-remotesigner's consensus HTTP API,
// api_version 1.x of the contract in its docs/contract.md, and a reader of its
// Prometheus metrics.
package remotesigner

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	requestTimeout = 30 * time.Second
	// maxBodyBytes bounds every response read; publicKeys holds 5,186 bytes
	// per key.
	maxBodyBytes = 16 << 20

	publicKeysPath   = "/api/v1/consensus/publicKeys"
	capabilitiesPath = "/api/v1/consensus/capabilities"
	signPath         = "/api/v1/consensus/sign/"
	upcheckPath      = "/upcheck"
	healthcheckPath  = "/healthcheck"
)

// Client calls the API with the sign token. The token is sent only as a
// bearer token: it never appears in an error or a log.
type Client struct {
	baseURL *url.URL
	http    *http.Client
	token   string
}

// New returns a client for the API at endpoint, an https URL whose
// certificate chains to a certificate of caPEM.
func New(endpoint string, caPEM []byte, token string) (*Client, error) {
	baseURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse remote signer endpoint: %w", err)
	}
	if baseURL.Scheme != "https" || baseURL.Host == "" {
		return nil, fmt.Errorf("remote signer endpoint %q must be an absolute https URL", endpoint)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("remote signer CA file holds no PEM certificate")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: requestTimeout, Transport: transport},
		token:   strings.TrimSpace(token),
	}, nil
}

// WithToken returns a client that sends token instead; an empty token sends
// no Authorization header.
func (client *Client) WithToken(token string) *Client {
	other := *client
	other.token = token
	return &other
}

// WithClientCertificate returns a client that presents the PEM certificate
// and key in every TLS handshake, as a validator client does when the signer
// runs in an mtls mode. The client it is called on keeps presenting none.
func (client *Client) WithClientCertificate(certPEM, keyPEM []byte) (*Client, error) {
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("load remote signer client certificate: %w", err)
	}
	// Clone copies the TLS configuration too.
	transport := client.http.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig.Certificates = []tls.Certificate{certificate}
	other := *client
	other.http = &http.Client{Timeout: client.http.Timeout, Transport: transport}
	return &other, nil
}

// ResponseError is an answer other than the expected 2xx. Its error string
// names the route, never the identifier or the token.
type ResponseError struct {
	Route      string
	StatusCode int
	// Reason and Message come from the contract's error body, when the body
	// is one.
	Reason  string
	Message string
	// Authenticate is the WWW-Authenticate header of a 401.
	Authenticate string
}

func (err *ResponseError) Error() string {
	if err.Reason == "" {
		return fmt.Sprintf("%s returned %d", err.Route, err.StatusCode)
	}
	return fmt.Sprintf("%s returned %d %s: %s", err.Route, err.StatusCode, err.Reason, err.Message)
}

// AsResponseError returns the ResponseError in err's chain, if any.
func AsResponseError(err error) (*ResponseError, bool) {
	var responseErr *ResponseError
	ok := errors.As(err, &responseErr)
	return responseErr, ok
}

// Upcheck succeeds once the signer has loaded its keys and opened its
// slashing-protection store.
func (client *Client) Upcheck(ctx context.Context) error {
	body, err := client.get(ctx, upcheckPath)
	if err != nil {
		return err
	}
	if text := strings.TrimSpace(string(body)); text != "OK" {
		return fmt.Errorf("GET %s answered %q, not OK", upcheckPath, text)
	}
	return nil
}

// PublicKeys returns the canonical keys the signer holds, in its order.
func (client *Client) PublicKeys(ctx context.Context) ([]string, error) {
	body, err := client.get(ctx, publicKeysPath)
	if err != nil {
		return nil, err
	}
	var keys []string
	if err := decodeStrict(body, &keys); err != nil {
		return nil, fmt.Errorf("decode %s: %w", publicKeysPath, err)
	}
	return keys, nil
}

// Capabilities is the signer's startup handshake with a validator client.
type Capabilities struct {
	APIVersion         string              `json:"api_version"`
	Types              []string            `json:"types"`
	RandaoReveal       bool                `json:"randao_reveal"`
	SelectionProofMode string              `json:"selection_proof_mode"`
	SlashingProtection string              `json:"slashing_protection"`
	Network            CapabilitiesNetwork `json:"network"`
}

// CapabilitiesNetwork is the chain the signer is bound to. Every uint64 is a
// decimal string; GenesisValidatorsRoot and GenesisTime are nil when the
// signer does not know them.
type CapabilitiesNetwork struct {
	ConfigName            string  `json:"config_name"`
	GenesisForkVersion    string  `json:"genesis_fork_version"`
	GenesisValidatorsRoot *string `json:"genesis_validators_root"`
	GenesisTime           *string `json:"genesis_time"`
	SecondsPerSlot        string  `json:"seconds_per_slot"`
	SlotsPerEpoch         string  `json:"slots_per_epoch"`
}

// Capabilities decodes strictly: the contract makes every added field a
// breaking change.
func (client *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	body, err := client.get(ctx, capabilitiesPath)
	if err != nil {
		return Capabilities{}, err
	}
	var capabilities Capabilities
	if err := decodeStrict(body, &capabilities); err != nil {
		return Capabilities{}, fmt.Errorf("decode %s: %w", capabilitiesPath, err)
	}
	return capabilities, nil
}

// Healthcheck is Web3Signer's health shape. Only a caller with the sign token
// sees the checks.
type Healthcheck struct {
	Status  string             `json:"status"`
	Outcome string             `json:"outcome"`
	Checks  []HealthcheckEntry `json:"checks"`
}

type HealthcheckEntry struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Healthcheck returns the signer's health. A signer that is down answers 503
// with the same shape, which is returned along with the error.
func (client *Client) Healthcheck(ctx context.Context) (Healthcheck, error) {
	status, body, err := client.Raw(ctx, healthcheckPath)
	if err != nil {
		return Healthcheck{}, err
	}
	var health Healthcheck
	decodeErr := json.Unmarshal(body, &health)
	if status != http.StatusOK {
		return health, &ResponseError{Route: "GET " + healthcheckPath, StatusCode: status}
	}
	if decodeErr != nil {
		return Healthcheck{}, fmt.Errorf("decode %s: %w", healthcheckPath, decodeErr)
	}
	return health, nil
}

// Raw returns the status and body of a GET of path, for diagnostics.
func (client *Client) Raw(ctx context.Context, path string) (int, []byte, error) {
	response, body, err := client.send(ctx, http.MethodGet, path, "GET "+path, nil)
	if err != nil {
		return 0, nil, err
	}
	return response.StatusCode, body, nil
}

// Sign posts a sign request for the key named by identifier, a canonical
// public key or its sha256 alias, and returns the signature.
func (client *Client) Sign(ctx context.Context, identifier string, request SignRequest) (string, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	// The route names the request: a canonical identifier is 5 KB long.
	route := "POST " + signPath + "{identifier}"
	response, body, err := client.send(ctx, http.MethodPost, signPath+identifier, route, payload)
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		return "", responseError(route, response, body)
	}
	var signed struct {
		Signature string `json:"signature"`
	}
	if err := decodeStrict(body, &signed); err != nil {
		return "", fmt.Errorf("decode %s: %w", route, err)
	}
	return signed.Signature, nil
}

func (client *Client) get(ctx context.Context, path string) ([]byte, error) {
	route := "GET " + path
	response, body, err := client.send(ctx, http.MethodGet, path, route, nil)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, responseError(route, response, body)
	}
	return body, nil
}

// send returns the response and its body; the body is already closed.
func (client *Client) send(ctx context.Context, method, path, route string, payload []byte) (*http.Response, []byte, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL.JoinPath(path).String(), body)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", route, err)
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if client.token != "" {
		request.Header.Set("Authorization", "Bearer "+client.token)
	}
	response, err := client.http.Do(request)
	if err != nil {
		// The URL of a sign request holds a 5 KB identifier; the route
		// names it instead.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, nil, fmt.Errorf("%s: %w", route, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", route, err)
	}
	return response, responseBody, nil
}

func responseError(route string, response *http.Response, body []byte) error {
	responseErr := &ResponseError{
		Route:        route,
		StatusCode:   response.StatusCode,
		Authenticate: response.Header.Get("WWW-Authenticate"),
	}
	var contractError struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Reason  string `json:"reason"`
	}
	// Strictly, so that a refusal whose body carries anything else, such as
	// a signature, has no reason.
	if decodeStrict(body, &contractError) == nil {
		responseErr.Reason = contractError.Reason
		responseErr.Message = contractError.Message
	}
	return responseErr
}

// decodeStrict decodes one JSON value and refuses unknown fields and
// trailing data.
func decodeStrict(body []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("trailing data after the JSON value")
	}
	return nil
}
