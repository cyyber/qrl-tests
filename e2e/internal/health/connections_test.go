package health

import (
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/sidecartest"
	"github.com/stretchr/testify/require"
)

const tcpTables = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:2162 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1001 1 0000000000000000 100 0 0 10 0
   1: 0B10AC0A:2162 0110AC0A:D431 01 00000000:00000000 00:00000000 00000000     0        0 1002 1 0000000000000000 20 4 30 10 -1
   2: 0B10AC0A:2161 0110AC0A:D432 01 00000010:00000000 00:00000000 00000000     0        0 1003 1 0000000000000000 20 4 30 10 -1
   3: 0B10AC0A:2162 0110AC0A:D433 06 00000000:00000000 03:00001770 00000000     0        0 0 3 0000000000000000
  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0000000000000000FFFF00000B10AC0A:2162 0000000000000000FFFF00000110AC0A:D434 01 003D0900:00000000 01:00000014 00000000     0        0 1004 1 0000000000000000 20 4 0 10 -1
`

func TestConnections(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.ExecOutput = tcpTables

	connections, err := Connections(t.Context(), docker, "node", 8546)
	require.NoError(t, err)
	require.Equal(t, [][]string{{"cat", "/proc/net/tcp", "/proc/net/tcp6"}}, docker.Execs)
	require.Equal(t, []Connection{
		{Remote: "0110AC0A:D431"},
		{Remote: "0000000000000000FFFF00000110AC0A:D434", SendQueue: 0x3D0900},
	}, connections, "the established ones on the port, without the listener and a closing one")
}

func TestConnectionsFailures(t *testing.T) {
	docker := sidecartest.NewDocker()
	docker.ExecOutput, docker.ExecExitCode = "cat: can't open '/proc/net/tcp6'", 1
	_, err := Connections(t.Context(), docker, "node", 8546)
	require.ErrorContains(t, err, "read the TCP connections of node")

	_, err = parseConnections("   1: 0B10AC0A:2162 0110AC0A:D431 01 nothex:00000000\n", 8546)
	require.ErrorContains(t, err, "send queue of the connection from 0110AC0A:D431")
}
