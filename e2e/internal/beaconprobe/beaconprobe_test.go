package beaconprobe

import (
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"testing"
	"time"

	"github.com/golang/snappy"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testDigest = [4]byte{1, 2, 3, 4}

// readRequest reads a request as the protocols frame one.
func readRequest(t *testing.T, stream network.Stream) []byte {
	length, err := binary.ReadUvarint(byteReader{stream})
	if !assert.NoError(t, err) {
		return nil
	}
	request := make([]byte, length)
	_, err = io.ReadFull(snappy.NewReader(stream), request)
	assert.NoError(t, err)
	return request
}

// writeChunk writes a chunk, with a block's context for a block.
func writeChunk(t *testing.T, stream network.Stream, code byte, body []byte) {
	_, err := stream.Write([]byte{code})
	assert.NoError(t, err)
	if code == codeSuccess && stream.Protocol() != statusProtocol {
		_, err = stream.Write(testDigest[:])
		assert.NoError(t, err)
	}
	assert.NoError(t, writeMessage(stream, body))
}

// startFakeNode starts a node that accepts any status and serves a request
// for blocks by range with serve.
func startFakeNode(t *testing.T, serve func(stream network.Stream, start, count, step uint64)) (address, id string) {
	t.Helper()
	node, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Security(noise.ID, noise.New),
		libp2p.DefaultMuxers,
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = node.Close() })
	node.SetStreamHandler(statusProtocol, func(stream network.Stream) {
		defer stream.Close()
		status := readRequest(t, stream)
		assert.Len(t, status, 84)
		assert.Equal(t, testDigest[:], status[:4])
		writeChunk(t, stream, codeSuccess, status)
	})
	node.SetStreamHandler(blocksByRangeProtocol, func(stream network.Stream) {
		defer stream.Close()
		request := readRequest(t, stream)
		if !assert.Len(t, request, 24) {
			return
		}
		serve(stream, binary.LittleEndian.Uint64(request), binary.LittleEndian.Uint64(request[8:]), binary.LittleEndian.Uint64(request[16:]))
	})
	return node.Addrs()[0].String(), node.ID().String()
}

func TestBlocksByRange(t *testing.T) {
	address, id := startFakeNode(t, func(stream network.Stream, start, count, step uint64) {
		assert.Equal(t, []uint64{7, 3, 1}, []uint64{start, count, step})
		for size := range count {
			// Blocks larger than one compressed frame holds.
			writeChunk(t, stream, codeSuccess, make([]byte, 100_000*(size+1)))
		}
	})
	conn, err := Dial(t.Context(), address, id, testDigest)
	require.NoError(t, err)
	defer conn.Close()

	response, err := conn.BlocksByRange(t.Context(), 7, 3, 1)
	require.NoError(t, err)
	require.Equal(t, 3, response.Chunks)
	require.Equal(t, uint64(600_000), response.Bytes)
	require.Equal(t, uint64(300_000), response.Largest)
	require.Zero(t, response.Code)
	require.Positive(t, response.Took)
}

func TestRefusedRequest(t *testing.T) {
	address, id := startFakeNode(t, func(stream network.Stream, _, _, _ uint64) {
		writeChunk(t, stream, codeSuccess, make([]byte, 10))
		writeChunk(t, stream, 1, []byte("rate limited"))
	})
	conn, err := Dial(t.Context(), address, id, testDigest)
	require.NoError(t, err)
	defer conn.Close()

	response, err := conn.BlocksByRange(t.Context(), 0, 2, 1)
	require.NoError(t, err)
	require.Equal(t, 1, response.Chunks)
	require.Equal(t, byte(1), response.Code)
	require.Equal(t, "rate limited", response.Error)
}

func TestForkDigest(t *testing.T) {
	// Of an all-zero version and root: the hash of 64 zero bytes.
	require.Equal(t, [4]byte{0xf5, 0xa5, 0xfd, 0x42}, ForkDigest([4]byte{}, [32]byte{}))
}

// TestLive runs against a real beacon node: LIVE_BEACON_P2P is its address,
// LIVE_BEACON_ID its peer ID and LIVE_FORK_DIGEST its fork digest in hex.
func TestLive(t *testing.T) {
	address := os.Getenv("LIVE_BEACON_P2P")
	if address == "" {
		t.Skip()
	}
	var digest [4]byte
	_, err := hexDecode(digest[:], os.Getenv("LIVE_FORK_DIGEST"))
	require.NoError(t, err)
	conn, err := Dial(t.Context(), address, os.Getenv("LIVE_BEACON_ID"), digest)
	require.NoError(t, err)
	defer conn.Close()
	for _, request := range [][3]uint64{{1, 8, 1}, {1, 64, 1}, {1, 65, 1}, {1, 2000, 1}, {1, 8, 2}} {
		started := time.Now()
		response, err := conn.BlocksByRange(t.Context(), request[0], request[1], request[2])
		t.Logf("from %d, count %d, step %d: %d chunks, %d bytes, largest %d, code %d %q, %s, error %v",
			request[0], request[1], request[2], response.Chunks, response.Bytes, response.Largest, response.Code, response.Error,
			time.Since(started).Round(time.Millisecond), err)
	}
}

func hexDecode(into []byte, text string) (int, error) {
	return hex.Decode(into, []byte(text))
}
