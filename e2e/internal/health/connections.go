package health

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
)

// tcpEstablished is the state /proc/net/tcp gives an established connection.
const tcpEstablished = "01"

// Connection is an established TCP connection of a container, as its kernel
// lists it.
type Connection struct {
	// Remote is the other end's address and port, in the kernel's hex. It
	// tells connections apart.
	Remote string
	// SendQueue is the bytes the container has written to the connection
	// that the other end has not yet acknowledged.
	SendQueue uint64
}

// Connections returns a running container's established TCP connections to
// one of its ports, from the kernel's tables of its network namespace. The
// container's image must have cat.
func Connections(ctx context.Context, docker sidecar.Executor, container string, port uint16) ([]Connection, error) {
	output, err := sidecar.Exec(ctx, docker, container, "cat", "/proc/net/tcp", "/proc/net/tcp6")
	if err != nil {
		return nil, fmt.Errorf("read the TCP connections of %s: %w", container, err)
	}
	return parseConnections(output, port)
}

// parseConnections reads the established connections to port from
// /proc/net/tcp and /proc/net/tcp6, whose lines are
// "sl local_address rem_address st tx_queue:rx_queue ...".
func parseConnections(table string, port uint16) ([]Connection, error) {
	local := fmt.Sprintf(":%04X", port)
	var connections []Connection
	for line := range strings.Lines(table) {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] == "sl" {
			continue
		}
		if !strings.HasSuffix(fields[1], local) || fields[3] != tcpEstablished {
			continue
		}
		sendQueue, _, _ := strings.Cut(fields[4], ":")
		queued, err := strconv.ParseUint(sendQueue, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("send queue of the connection from %s: %w", fields[2], err)
		}
		connections = append(connections, Connection{Remote: fields[2], SendQueue: queued})
	}
	return connections, nil
}
