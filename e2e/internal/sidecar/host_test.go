package sidecar

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostURL(t *testing.T) {
	for _, test := range []struct {
		endpoint string
		want     string
		wantErr  string
	}{
		{endpoint: "http://127.0.0.1:3500", want: "http://host.docker.internal:3500"},
		{endpoint: "ws://[::1]:8546/path?x=1", want: "ws://host.docker.internal:8546/path?x=1"},
		{endpoint: "http://127.0.0.1", wantErr: `URL "http://127.0.0.1" must include a scheme, host, and port`},
		{endpoint: "localhost:3500", wantErr: `URL "localhost:3500" must include a scheme, host, and port`},
		{endpoint: "http://%zz", wantErr: `parse "http://%zz": invalid URL escape "%zz"`},
	} {
		t.Run(test.endpoint, func(t *testing.T) {
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

func TestHostAddress(t *testing.T) {
	for _, test := range []struct {
		address string
		want    string
		wantErr string
	}{
		{address: "127.0.0.1:4000", want: "host.docker.internal:4000"},
		{address: "[::1]:4000", want: "host.docker.internal:4000"},
		{address: "127.0.0.1", wantErr: "parse host:port: address 127.0.0.1: missing port in address"},
		{address: "127.0.0.1:", wantErr: `address "127.0.0.1:" must include a port`},
	} {
		t.Run(test.address, func(t *testing.T) {
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
