package staker

import (
	"archive/tar"
	"bytes"
	"io"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
)

func TestRewritePublishedEndpoints(t *testing.T) {
	url, err := rewritePublishedURL("http://127.0.0.1:3500")
	require.NoError(t, err)
	require.Equal(t, "http://host.docker.internal:3500", url)

	host, err := rewritePublishedHost("127.0.0.1:4000")
	require.NoError(t, err)
	require.Equal(t, "host.docker.internal:4000", host)

	_, err = rewritePublishedURL("http://127.0.0.1")
	require.ErrorContains(t, err, "scheme, host, and port")

	_, err = rewritePublishedHost("127.0.0.1")
	require.ErrorContains(t, err, "host:port")
}

func TestParseAuthToken(t *testing.T) {
	token, err := parseAuthToken([]byte("# jwt token\n0xabc\n"))
	require.NoError(t, err)
	require.Equal(t, "0xabc", token)

	_, err = parseAuthToken([]byte("# only comments\n\n"))
	require.ErrorContains(t, err, "empty")
}

func TestOperatorFixtureArchive(t *testing.T) {
	archive, err := operatorFixtureArchive([]byte("PRESET_BASE: minimal\n"))
	require.NoError(t, err)

	files := readTarNames(t, archive)
	require.Equal(t, []string{
		"start-validator.sh",
		"wallet-password.txt",
		"network-configs",
		"network-configs/config.yaml",
	}, files)

	_, err = operatorFixtureArchive(nil)
	require.ErrorContains(t, err, "empty")
}

func TestPublishedHostPort(t *testing.T) {
	port, ok := network.PortFrom(operatorGatewayPort, network.TCP)
	require.True(t, ok)

	hostPort, err := publishedHostPort(containertypes.InspectResponse{
		NetworkSettings: &containertypes.NetworkSettings{
			Ports: network.PortMap{port: {{HostPort: "32765"}}},
		},
	}, operatorGatewayPort)
	require.NoError(t, err)
	require.Equal(t, "32765", hostPort)

	_, err = publishedHostPort(containertypes.InspectResponse{}, operatorGatewayPort)
	require.ErrorContains(t, err, "network settings")
}

func readTarNames(t *testing.T, archive []byte) []string {
	t.Helper()
	reader := tar.NewReader(bytes.NewReader(archive))
	var names []string
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return names
		}
		require.NoError(t, err)
		names = append(names, header.Name)
	}
}
