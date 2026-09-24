package sidecar

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostURL(t *testing.T) {
	for _, test := range []struct {
		name     string
		endpoint string
		want     string
		wantErr  string
	}{
		{name: "IPv4", endpoint: "http://127.0.0.1:3500", want: "http://host.docker.internal:3500"},
		{
			name:     "WebSocket on IPv6 keeps path and query",
			endpoint: "ws://[::1]:8546/path?x=1",
			want:     "ws://host.docker.internal:8546/path?x=1",
		},
		{
			name:     "missing port",
			endpoint: "http://127.0.0.1",
			wantErr:  `URL "http://127.0.0.1" must include a scheme, host, and port`,
		},
		{
			name:     "missing scheme",
			endpoint: "localhost:3500",
			wantErr:  `URL "localhost:3500" must include a scheme, host, and port`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := HostURL(test.endpoint)
			if test.wantErr != "" {
				require.EqualError(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestHostURLRejectsMalformedURL(t *testing.T) {
	_, err := HostURL("http://%zz")
	var parseErr *url.Error
	require.ErrorAs(t, err, &parseErr)
}

func TestHostAddress(t *testing.T) {
	for _, test := range []struct {
		name    string
		address string
		want    string
		wantErr string
	}{
		{name: "IPv4", address: "127.0.0.1:4000", want: "host.docker.internal:4000"},
		{name: "IPv6", address: "[::1]:4000", want: "host.docker.internal:4000"},
		{name: "empty port", address: "127.0.0.1:", wantErr: `address "127.0.0.1:" must include a port`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := HostAddress(test.address)
			if test.wantErr != "" {
				require.EqualError(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestHostAddressRejectsMissingPort(t *testing.T) {
	_, err := HostAddress("127.0.0.1")
	require.ErrorContains(t, err, "parse host:port: ")
}
