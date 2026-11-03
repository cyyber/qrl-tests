package remotesigner

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const testToken = "0x" + "5a" + "0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f"

// The capabilities example of go-remotesigner's docs/contract.md, section
// "Capabilities and `api_version`".
const capabilitiesBody = `{"api_version":"1.0.0",
 "types":["BLOCK_V2","ATTESTATION","AGGREGATION_SLOT","AGGREGATE_AND_PROOF","SYNC_COMMITTEE_MESSAGE",
          "SYNC_COMMITTEE_SELECTION_PROOF","SYNC_COMMITTEE_CONTRIBUTION_AND_PROOF","VALIDATOR_REGISTRATION"],
 "randao_reveal":true,"selection_proof_mode":"deterministic","slashing_protection":"bolt",
 "network":{"config_name":"end-to-end","genesis_fork_version":"0x000000fd",
            "genesis_validators_root":"0x9c2a5e0d6b3f4a1e8c7d6b5a4f3e2d1c0b9a8f7e6d5c4b3a2918070605040302",
            "genesis_time":"1760000000","seconds_per_slot":"10","slots_per_epoch":"6"}}`

func TestClientReadsTheContractShapes(t *testing.T) {
	key := "0x" + strings.Repeat("ab", 2592)
	server := newSigner(t, map[string]http.HandlerFunc{
		"GET /upcheck": func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(writer, "OK")
		},
		"GET /api/v1/consensus/publicKeys": func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, `["`+key+`"]`)
		},
		"GET /api/v1/consensus/capabilities": func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, capabilitiesBody)
		},
		"GET /healthcheck": func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, `{"status":"UP","outcome":"UP","checks":[{"id":"default-check","status":"UP"},
				{"id":"keys-check/keystores-bulk-loading","status":"UP","data":{"keys-loaded":64,"error-count":0}}]}`)
		},
	})
	client := server.client(t, testToken)

	require.NoError(t, client.Upcheck(t.Context()))
	require.Equal(t, []string{key}, must(client.PublicKeys(t.Context())))

	capabilities := must(client.Capabilities(t.Context()))
	require.Equal(t, "1.0.0", capabilities.APIVersion)
	require.True(t, capabilities.RandaoReveal)
	require.Equal(t, "bolt", capabilities.SlashingProtection)
	require.Len(t, capabilities.Types, 8)
	require.Equal(t, "0x000000fd", capabilities.Network.GenesisForkVersion)
	require.Equal(t, "1760000000", *capabilities.Network.GenesisTime)
	require.Equal(t, "6", capabilities.Network.SlotsPerEpoch)

	health := must(client.Healthcheck(t.Context()))
	require.Equal(t, "UP", health.Status)
	require.Equal(t, []HealthcheckEntry{
		{ID: "default-check", Status: "UP"}, {ID: "keys-check/keystores-bulk-loading", Status: "UP"},
	}, health.Checks)

	status, body, err := client.Raw(t.Context(), "/api/v1/consensus/capabilities")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.JSONEq(t, capabilitiesBody, string(body))

	for _, request := range server.requests {
		require.Equal(t, "Bearer "+testToken, request.Header.Get("Authorization"), request.URL.Path)
	}
}

func TestClientRefusesUnknownCapabilities(t *testing.T) {
	server := newSigner(t, map[string]http.HandlerFunc{
		"GET /api/v1/consensus/capabilities": func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, strings.Replace(capabilitiesBody, `"randao_reveal":true`, `"randao_reveal":true,"extra":1`, 1))
		},
	})
	_, err := server.client(t, testToken).Capabilities(t.Context())
	require.ErrorContains(t, err, `unknown field "extra"`)
}

func TestClientReportsContractErrors(t *testing.T) {
	server := newSigner(t, map[string]http.HandlerFunc{
		"GET /api/v1/consensus/publicKeys": func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("WWW-Authenticate", "Bearer")
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(writer, `{"code":401,"message":"unauthenticated","reason":"UNAUTHENTICATED"}`)
		},
		"GET /healthcheck": func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(writer, `{"status":"DOWN","outcome":"DOWN"}`)
		},
	})
	client := server.client(t, testToken)

	_, err := client.WithToken("").PublicKeys(t.Context())
	responseErr, ok := AsResponseError(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, &ResponseError{
		Route: "GET /api/v1/consensus/publicKeys", StatusCode: http.StatusUnauthorized,
		Reason: "UNAUTHENTICATED", Message: "unauthenticated", Authenticate: "Bearer",
	}, responseErr)
	require.Empty(t, server.requests[0].Header.Get("Authorization"))

	_, err = client.WithToken("0xwrong").PublicKeys(t.Context())
	require.Equal(t, "Bearer 0xwrong", server.requests[1].Header.Get("Authorization"))
	require.NotContains(t, err.Error(), "0xwrong")

	health, err := client.Healthcheck(t.Context())
	require.Equal(t, "DOWN", health.Status)
	responseErr, ok = AsResponseError(err)
	require.True(t, ok)
	require.Equal(t, http.StatusServiceUnavailable, responseErr.StatusCode)
}

func TestClientSigns(t *testing.T) {
	key := "0x" + strings.Repeat("cd", 2592)
	signature := "0x" + strings.Repeat("ef", 4627)
	var requests []map[string]any
	server := newSigner(t, map[string]http.HandlerFunc{
		"POST /api/v1/consensus/sign/{identifier}": func(writer http.ResponseWriter, request *http.Request) {
			require.Equal(t, key, request.PathValue("identifier"))
			require.Equal(t, "application/json", request.Header.Get("Content-Type"))
			var body map[string]any
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			requests = append(requests, body)
			if body["type"] == TypeBlockV2 {
				writer.WriteHeader(http.StatusPreconditionFailed)
				_, _ = io.WriteString(writer, `{"code":412,"message":"slashing protection refused the request","reason":"`+ReasonDoubleProposal+`"}`)
				return
			}
			_, _ = io.WriteString(writer, `{"signature":"`+signature+`"}`)
		},
	})
	client := server.client(t, testToken)
	forkInfo := GenesisForkInfo("0x000000fd", "0x"+strings.Repeat("01", 32))

	signed, err := client.Sign(t.Context(), key, AttestationRequest(forkInfo, AttestationData{
		Slot: 61, Index: 2, BeaconBlockRoot: "0x" + strings.Repeat("02", 32),
		Source: Checkpoint{Epoch: 9, Root: "0x" + strings.Repeat("03", 32)},
		Target: Checkpoint{Epoch: 10, Root: "0x" + strings.Repeat("04", 32)},
	}))
	require.NoError(t, err)
	require.Equal(t, signature, signed)
	wantAttestation := `{"type":"ATTESTATION",
		"fork_info":{"fork":{"previous_version":"0x000000fd","current_version":"0x000000fd","epoch":"0"},
		             "genesis_validators_root":"0x` + strings.Repeat("01", 32) + `"},
		"attestation":{"slot":"61","index":"2","beacon_block_root":"0x` + strings.Repeat("02", 32) + `",
		               "source":{"epoch":"9","root":"0x` + strings.Repeat("03", 32) + `"},
		               "target":{"epoch":"10","root":"0x` + strings.Repeat("04", 32) + `"}}}`
	require.JSONEq(t, wantAttestation, string(must(json.Marshal(requests[0]))))

	_, err = client.Sign(t.Context(), key, BlockRequest(forkInfo, BeaconBlockHeader{
		Slot: 62, ProposerIndex: 7, ParentRoot: "0x" + strings.Repeat("05", 32),
		StateRoot: "0x" + strings.Repeat("06", 32), BodyRoot: "0x" + strings.Repeat("07", 32),
	}))
	responseErr, ok := AsResponseError(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, http.StatusPreconditionFailed, responseErr.StatusCode)
	require.Equal(t, "SLASHING_DOUBLE_PROPOSAL", responseErr.Reason)
	// The route names the request; the 5 KB identifier stays out of errors.
	require.Equal(t,
		"POST /api/v1/consensus/sign/{identifier} returned 412 SLASHING_DOUBLE_PROPOSAL: slashing protection refused the request",
		err.Error(),
	)
	wantBlock := `{"type":"BLOCK_V2",
		"fork_info":{"fork":{"previous_version":"0x000000fd","current_version":"0x000000fd","epoch":"0"},
		             "genesis_validators_root":"0x` + strings.Repeat("01", 32) + `"},
		"beacon_block":{"version":"ZOND","block_header":{"slot":"62","proposer_index":"7",
		  "parent_root":"0x` + strings.Repeat("05", 32) + `","state_root":"0x` + strings.Repeat("06", 32) + `",
		  "body_root":"0x` + strings.Repeat("07", 32) + `"}}}`
	require.JSONEq(t, wantBlock, string(must(json.Marshal(requests[1]))))
}

func TestNewChecksTheEndpointAndCA(t *testing.T) {
	_, err := New("http://127.0.0.1:9000", nil, testToken)
	require.ErrorContains(t, err, "must be an absolute https URL")

	_, err = New("https://127.0.0.1:9000", []byte("not a certificate"), testToken)
	require.ErrorContains(t, err, "holds no PEM certificate")

	// A certificate another CA issued is refused.
	server := newSigner(t, map[string]http.HandlerFunc{"GET /upcheck": func(http.ResponseWriter, *http.Request) {}})
	otherCA, _ := newCertificateAuthority(t)
	client := must(New(server.URL, otherCA, testToken))
	require.ErrorContains(t, client.Upcheck(t.Context()), "certificate signed by unknown authority")
	require.Empty(t, server.requests)
}

func TestClientPresentsItsClientCertificate(t *testing.T) {
	server := newMTLSSigner(t, map[string]http.HandlerFunc{
		"GET /upcheck": func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, "OK")
		},
	})
	withoutCertificate := server.client(t, testToken)
	client := must(withoutCertificate.WithClientCertificate(server.clientCertPEM, server.clientKeyPEM))

	require.NoError(t, client.Upcheck(t.Context()))
	require.Len(t, server.requests, 1)
	require.NotEmpty(t, server.requests[0].TLS.PeerCertificates)
	require.Equal(t, "validator client", server.requests[0].TLS.PeerCertificates[0].Subject.CommonName)
	require.Equal(t, "Bearer "+testToken, server.requests[0].Header.Get("Authorization"))
	// Another token keeps the certificate.
	require.NoError(t, client.WithToken("").Upcheck(t.Context()))
	require.Empty(t, server.requests[1].Header.Get("Authorization"))

	// The handshake fails without one: there is no HTTP answer, so no
	// ResponseError.
	err := withoutCertificate.Upcheck(t.Context())
	require.Error(t, err)
	_, ok := AsResponseError(err)
	require.False(t, ok, "%v", err)
	require.Len(t, server.requests, 2)
}

func TestClientRefusesAMismatchedClientKey(t *testing.T) {
	server := newMTLSSigner(t, map[string]http.HandlerFunc{})
	_, otherKey := newTestCA(t).clientCertificate(t)
	_, err := server.client(t, testToken).WithClientCertificate(server.clientCertPEM, otherKey)
	require.ErrorContains(t, err, "load remote signer client certificate")
	require.NotContains(t, err.Error(), string(otherKey))
}

func TestClientRefusesASignatureInARefusal(t *testing.T) {
	server := newSigner(t, map[string]http.HandlerFunc{
		"POST /api/v1/consensus/sign/{identifier}": func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusPreconditionFailed)
			_, _ = io.WriteString(writer, `{"code":412,"message":"slashing protection refused the request",`+
				`"reason":"SLASHING_DOUBLE_VOTE","signature":"0x00"}`)
		},
	})
	forkInfo := GenesisForkInfo("0x000000fd", "0x"+strings.Repeat("01", 32))
	_, err := server.client(t, testToken).Sign(t.Context(), "0x"+strings.Repeat("ab", 32),
		AttestationRequest(forkInfo, AttestationData{}))
	responseErr, ok := AsResponseError(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, http.StatusPreconditionFailed, responseErr.StatusCode)
	require.Empty(t, responseErr.Reason)
}

type fakeSigner struct {
	*httptest.Server
	caPEM    []byte
	requests []*http.Request
	// clientCertPEM and clientKeyPEM are a client certificate of the
	// signer's CA, which an mtls signer requires.
	clientCertPEM []byte
	clientKeyPEM  []byte
}

// newSigner serves routes over TLS with a certificate for 127.0.0.1 that a
// CA of its own issued, as qrl-package sets the signer up.
func newSigner(t *testing.T, routes map[string]http.HandlerFunc) *fakeSigner {
	t.Helper()
	return startSigner(t, routes, false)
}

// newMTLSSigner is newSigner in an mtls mode: the handshake requires a
// client certificate of the signer's CA.
func newMTLSSigner(t *testing.T, routes map[string]http.HandlerFunc) *fakeSigner {
	t.Helper()
	return startSigner(t, routes, true)
}

func startSigner(t *testing.T, routes map[string]http.HandlerFunc, requireClientCertificate bool) *fakeSigner {
	t.Helper()
	signer := new(fakeSigner)
	mux := http.NewServeMux()
	for pattern, handler := range routes {
		mux.HandleFunc(pattern, func(writer http.ResponseWriter, request *http.Request) {
			signer.requests = append(signer.requests, request)
			handler(writer, request)
		})
	}
	authority := newTestCA(t)
	signer.caPEM = authority.pem
	signer.Server = httptest.NewUnstartedServer(mux)
	signer.TLS = &tls.Config{Certificates: []tls.Certificate{authority.serverCertificate(t)}, MinVersion: tls.VersionTLS13}
	if requireClientCertificate {
		roots := x509.NewCertPool()
		roots.AddCert(authority.certificate)
		signer.TLS.ClientCAs = roots
		signer.TLS.ClientAuth = tls.RequireAndVerifyClientCert
		signer.clientCertPEM, signer.clientKeyPEM = authority.clientCertificate(t)
	}
	signer.StartTLS()
	t.Cleanup(signer.Close)
	return signer
}

func (signer *fakeSigner) client(t *testing.T, token string) *Client {
	t.Helper()
	return must(New(signer.URL, signer.caPEM, token))
}

// newCertificateAuthority returns the PEM certificate of a new CA and a
// server certificate it issued for 127.0.0.1.
func newCertificateAuthority(t *testing.T) ([]byte, tls.Certificate) {
	t.Helper()
	authority := newTestCA(t)
	return authority.pem, authority.serverCertificate(t)
}

type testCA struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
	pem         []byte
	serial      int64
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key := must(ecdsa.GenerateKey(elliptic.P256(), rand.Reader))
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "signer CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der := must(x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key))
	return &testCA{
		certificate: must(x509.ParseCertificate(der)),
		key:         key,
		pem:         pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		serial:      1,
	}
}

// issue returns a certificate the CA issued for template, and its key.
func (authority *testCA) issue(t *testing.T, template *x509.Certificate) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	authority.serial++
	template.SerialNumber = big.NewInt(authority.serial)
	template.NotBefore = time.Now().Add(-time.Hour)
	template.NotAfter = time.Now().Add(time.Hour)
	template.KeyUsage = x509.KeyUsageDigitalSignature
	key := must(ecdsa.GenerateKey(elliptic.P256(), rand.Reader))
	return must(x509.CreateCertificate(rand.Reader, template, authority.certificate, &key.PublicKey, authority.key)), key
}

// serverCertificate is a server certificate for 127.0.0.1.
func (authority *testCA) serverCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	der, key := authority.issue(t, &x509.Certificate{
		Subject:     pkix.Name{CommonName: "signer"},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
	})
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// clientCertificate is a client certificate and its key, in PEM.
func (authority *testCA) clientCertificate(t *testing.T) ([]byte, []byte) {
	t.Helper()
	der, key := authority.issue(t, &x509.Certificate{
		Subject:     pkix.Name{CommonName: "validator client"},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: must(x509.MarshalPKCS8PrivateKey(key))})
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}
