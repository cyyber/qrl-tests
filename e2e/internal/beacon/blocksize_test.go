package beacon

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/signing"
	"github.com/stretchr/testify/require"
)

// encodeBlock lays a signed block out as qrysm encodes it, with a body whose
// variable fields are the given number of bytes long.
func encodeBlock(fields [bodyOffsets]int) []byte {
	body := make([]byte, bodyFixed)
	offset := bodyFixed
	for field, size := range fields {
		binary.LittleEndian.PutUint32(body[bodyFixedFields+4*field:], uint32(offset))
		offset += size
	}
	body = append(body, make([]byte, offset-bodyFixed)...)

	block := make([]byte, blockFixed)
	binary.LittleEndian.PutUint32(block[blockFixed-4:], blockFixed)
	block = append(block, body...)

	signed := make([]byte, signedBlockFixed)
	binary.LittleEndian.PutUint32(signed, signedBlockFixed)
	return append(signed, block...)
}

func TestParseBlockSections(t *testing.T) {
	encoded := encodeBlock([bodyOffsets]int{9478, 0, 148209, 8379, 4643, 20 + 3*signing.SignatureLength, 1000})
	sections, err := ParseBlockSections(encoded)
	require.NoError(t, err)
	require.Equal(t, BlockSections{
		Total:             uint64(len(encoded)),
		ProposerSlashings: 9478,
		Attestations:      148209,
		Deposits:          8379,
		VoluntaryExits:    4643,
		SyncAggregate:     20 + 3*signing.SignatureLength,
		ExecutionPayload:  1000,
	}, sections)
	// The block's offset and signature, its fixed fields and the body's.
	require.Equal(t, uint64(4631+84+164), sections.Rest())

	_, err = ParseBlockSections(encoded[:100])
	require.ErrorContains(t, err, "shorter than its fixed part")

	broken := encodeBlock([bodyOffsets]int{})
	binary.LittleEndian.PutUint32(broken[signedBlockFixed+blockFixed+bodyFixedFields+4:], 1<<20)
	_, err = ParseBlockSections(broken)
	require.ErrorContains(t, err, "is past the next")
}

func TestFullOperationSizes(t *testing.T) {
	// qrysm's mainnet constants.
	spec := Spec{
		"MAX_PROPOSER_SLASHINGS": "16", "MAX_ATTESTER_SLASHINGS": "2", "MAX_ATTESTATIONS": "4", "MAX_DEPOSITS": "16",
		"MAX_VOLUNTARY_EXITS": "16", "MAX_VALIDATORS_PER_COMMITTEE": "32", "SYNC_COMMITTEE_SIZE": "128",
	}
	limits, err := spec.OperationLimits()
	require.NoError(t, err)
	// Each list's elements as qrysm's generated SizeSSZ counts them: a
	// proposer slashing of 9,478 bytes; an attester slashing's offset, its two
	// offsets and two indexed attestations of 136 + 32*8 + 32*4,627; an
	// attestation's offset, 136 bytes, 5 of bits and 32 signatures; a deposit
	// of 8,379; an exit of 4,643; and the sync aggregate's 20 bytes and 128
	// signatures.
	full := limits.Full()
	require.Equal(t, BlockSections{
		ProposerSlashings: 16 * 9478,
		AttesterSlashings: 2 * (4 + 8 + 2*(136+32*8+32*4627)),
		Attestations:      4 * (4 + 136 + 5 + 32*4627),
		Deposits:          16 * 8379,
		VoluntaryExits:    16 * 4643,
		SyncAggregate:     20 + 128*4627,
	}, full)
	require.Equal(t, uint64(2_138_960), full.Operations())
	require.Equal(t, uint64(20+100*4627), limits.SyncAggregateSize(100))

	delete(spec, "SYNC_COMMITTEE_SIZE")
	_, err = spec.OperationLimits()
	require.ErrorContains(t, err, "does not define SYNC_COMMITTEE_SIZE")
}

func TestClientReadsBlockSections(t *testing.T) {
	encoded := encodeBlock([bodyOffsets]int{0, 0, 0, 0, 0, 20, 512})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/qrl/v1/beacon/blocks/7" && request.Header.Get("Accept") == sszMediaType:
			writer.Header().Set("Content-Type", sszMediaType)
			_, _ = writer.Write(encoded)
		case request.URL.Path == "/qrl/v1/beacon/blocks/9":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"data":{}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := New(server.URL)
	require.NoError(t, err)

	sections, err := client.BlockSections(t.Context(), "7")
	require.NoError(t, err)
	require.Equal(t, BlockSections{Total: uint64(len(encoded)), SyncAggregate: 20, ExecutionPayload: 512}, sections)
	_, err = client.BlockSections(t.Context(), "8")
	require.True(t, IsNotFound(err))
	_, err = client.BlockSections(t.Context(), "9")
	require.ErrorContains(t, err, "not SSZ")
}
