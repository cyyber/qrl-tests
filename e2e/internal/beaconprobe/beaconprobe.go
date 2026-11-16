// Package beaconprobe speaks a beacon node's request protocols as a peer, to
// measure the responses it serves and the requests it refuses.
//
// It carries the few message layouts it needs: importing qrysm's own would
// pull the whole client into the tests' module.
package beaconprobe

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/golang/snappy"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/multiformats/go-multiaddr"
)

const (
	protocolPrefix = "/consensus/beacon_chain/req"
	encoding       = "/ssz_snappy"

	statusProtocol        = protocol.ID(protocolPrefix + "/status/1" + encoding)
	blocksByRangeProtocol = protocol.ID(protocolPrefix + "/beacon_blocks_by_range/2" + encoding)
	blocksByRootProtocol  = protocol.ID(protocolPrefix + "/beacon_blocks_by_root/2" + encoding)

	// A block's chunk names its fork, by the fork's digest, before the block.
	blockContext = 4

	// A chunk's first byte: zero for a block, otherwise an error's code.
	codeSuccess = 0

	// The largest chunk a peer accepts, which bounds what the probe reads.
	maxChunk = 10 << 20
)

// Conn is a connection to a beacon node that has accepted the probe as a
// peer.
type Conn struct {
	host host.Host
	node peer.ID
}

// ForkDigest returns the digest that names a network's fork in its peers'
// status: the first bytes of the hash tree root of the fork version and the
// genesis validators root.
func ForkDigest(forkVersion [4]byte, genesisValidatorsRoot [32]byte) [4]byte {
	var chunks [64]byte
	copy(chunks[:], forkVersion[:])
	copy(chunks[32:], genesisValidatorsRoot[:])
	root := sha256.Sum256(chunks[:])
	return [4]byte(root[:4])
}

// Dial connects to the beacon node with the given peer ID at a TCP address,
// and gives it the status of a peer at genesis on the fork, which every node
// of the network accepts. A node disconnects a peer that sends no status.
func Dial(ctx context.Context, address string, nodeID string, forkDigest [4]byte) (*Conn, error) {
	id, err := peer.Decode(nodeID)
	if err != nil {
		return nil, fmt.Errorf("parse peer ID: %w", err)
	}
	target, err := multiaddr.NewMultiaddr(address)
	if err != nil {
		return nil, fmt.Errorf("parse address: %w", err)
	}
	key, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	probe, err := libp2p.New(
		libp2p.Identity(key),
		libp2p.NoListenAddrs,
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Security(noise.ID, noise.New),
		libp2p.DefaultMuxers,
	)
	if err != nil {
		return nil, fmt.Errorf("create host: %w", err)
	}
	conn := &Conn{host: probe, node: id}
	if err := probe.Connect(ctx, peer.AddrInfo{ID: id, Addrs: []multiaddr.Multiaddr{target}}); err != nil {
		_ = probe.Close()
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := conn.status(ctx, forkDigest); err != nil {
		_ = probe.Close()
		return nil, fmt.Errorf("status: %w", err)
	}
	return conn, nil
}

// Close closes the connection.
func (conn *Conn) Close() error {
	return conn.host.Close()
}

// status sends the status of a peer at genesis: a zero finalized checkpoint
// and head.
func (conn *Conn) status(ctx context.Context, forkDigest [4]byte) error {
	var status [84]byte
	copy(status[:], forkDigest[:])
	response, err := conn.request(ctx, statusProtocol, status[:], 0)
	if err != nil {
		return err
	}
	if response.Code != codeSuccess {
		return fmt.Errorf("the node refused the status: %s", response.Error)
	}
	return nil
}

// Response is a node's response to a request.
type Response struct {
	// Chunks is how many chunks the response has, one for each block, and
	// Bytes their length uncompressed, as a peer holds them to its limits.
	Chunks int
	Bytes  uint64
	// Largest is the length of the largest chunk.
	Largest uint64
	// Code is zero for a response the node served, or the code of its
	// refusal, with Error its reason.
	Code  byte
	Error string
	// Took is how long the response took to arrive in full.
	Took time.Duration
}

// BlocksByRange requests count blocks from a slot on, every step slots.
func (conn *Conn) BlocksByRange(ctx context.Context, start, count, step uint64) (Response, error) {
	var request [24]byte
	binary.LittleEndian.PutUint64(request[:], start)
	binary.LittleEndian.PutUint64(request[8:], count)
	binary.LittleEndian.PutUint64(request[16:], step)
	return conn.request(ctx, blocksByRangeProtocol, request[:], blockContext)
}

// BlocksByRoot requests the blocks with the given roots.
func (conn *Conn) BlocksByRoot(ctx context.Context, roots [][32]byte) (Response, error) {
	request := make([]byte, 0, 32*len(roots))
	for _, root := range roots {
		request = append(request, root[:]...)
	}
	return conn.request(ctx, blocksByRootProtocol, request, blockContext)
}

// request sends an SSZ-encoded request on a new stream and reads the whole
// response, whose chunks carry the given bytes of context.
func (conn *Conn) request(ctx context.Context, id protocol.ID, encoded []byte, chunkContext int) (Response, error) {
	started := time.Now()
	stream, err := conn.host.NewStream(ctx, conn.node, id)
	if err != nil {
		return Response{}, fmt.Errorf("open stream: %w", err)
	}
	defer func() { _ = stream.Reset() }()
	if deadline, ok := ctx.Deadline(); ok {
		_ = stream.SetDeadline(deadline)
	}

	if err := writeMessage(stream, encoded); err != nil {
		return Response{}, fmt.Errorf("send request: %w", err)
	}
	if err := stream.CloseWrite(); err != nil {
		return Response{}, fmt.Errorf("end request: %w", err)
	}
	response, err := readResponse(stream, chunkContext)
	response.Took = time.Since(started)
	return response, err
}

// writeMessage writes a message as the protocols frame one: its length, then
// its bytes compressed.
func writeMessage(stream io.Writer, message []byte) error {
	if _, err := stream.Write(binary.AppendUvarint(nil, uint64(len(message)))); err != nil {
		return err
	}
	compressed := snappy.NewBufferedWriter(stream)
	if _, err := compressed.Write(message); err != nil {
		return err
	}
	return compressed.Close()
}

// readResponse reads chunks until the node ends the stream or refuses.
func readResponse(stream io.Reader, chunkContext int) (Response, error) {
	var response Response
	for {
		var code [1]byte
		if _, err := io.ReadFull(stream, code[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, network.ErrReset) {
				return response, nil
			}
			return response, fmt.Errorf("read chunk %d: %w", response.Chunks, err)
		}
		// A refusal has no context.
		if code[0] == codeSuccess {
			if _, err := io.CopyN(io.Discard, stream, int64(chunkContext)); err != nil {
				return response, fmt.Errorf("read the context of chunk %d: %w", response.Chunks, err)
			}
		}
		length, err := binary.ReadUvarint(byteReader{stream})
		if err != nil {
			return response, fmt.Errorf("read the length of chunk %d: %w", response.Chunks, err)
		}
		if length > maxChunk {
			return response, fmt.Errorf("chunk %d is %d bytes, more than a peer accepts", response.Chunks, length)
		}
		// Each chunk is a compressed stream of its own, which the reader
		// does not read past.
		body := io.LimitReader(snappy.NewReader(stream), int64(length))
		if code[0] != codeSuccess {
			reason, err := io.ReadAll(body)
			response.Code, response.Error = code[0], string(reason)
			return response, err
		}
		read, err := io.Copy(io.Discard, body)
		if err != nil || uint64(read) != length {
			return response, fmt.Errorf("read chunk %d: %d of %d bytes: %w", response.Chunks, read, length, err)
		}
		response.Chunks++
		response.Bytes += length
		response.Largest = max(response.Largest, length)
	}
}

// byteReader reads a stream a byte at a time, so that reading a length takes
// nothing of what follows it.
type byteReader struct{ io.Reader }

func (reader byteReader) ReadByte() (byte, error) {
	var single [1]byte
	_, err := io.ReadFull(reader.Reader, single[:])
	return single[0], err
}
