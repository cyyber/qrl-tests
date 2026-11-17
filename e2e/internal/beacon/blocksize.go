package beacon

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/cyyber/qrl-tests/e2e/internal/signing"
)

// The layout of qrysm's SignedBeaconBlockZond in SSZ: the block's offset and
// signature, the block's fixed fields with the body's offset last, then the
// body's fixed fields (RANDAO reveal, execution data, graffiti) and the
// offsets of its seven variable fields.
const (
	signedBlockFixed = 4 + signing.SignatureLength
	blockFixed       = 8 + 8 + 32 + 32 + 4
	bodyFixedFields  = 32 + 72 + 32
	bodyOffsets      = 7
	bodyFixed        = bodyFixedFields + 4*bodyOffsets
)

// SSZ sizes of qrysm's operations, as its generated code counts them.
const (
	signedHeaderSize     = 112 + signing.SignatureLength
	proposerSlashingSize = 2 * signedHeaderSize
	// An attestation and an indexed attestation hold, besides their lists,
	// two offsets and the attestation data.
	attestationFixed = 4 + 128 + 4
	// A deposit's proof has a branch of the deposit tree's 32 levels and its
	// length.
	depositSize       = 33*32 + signing.PublicKeyLength + signing.WithdrawalRecipientLength + 8 + signing.RandaoCommitmentLength + signing.SignatureLength
	voluntaryExitSize = 16 + signing.SignatureLength
	listOffset        = 4
)

// BlockSections is the size of each part of a signed block's SSZ encoding.
type BlockSections struct {
	Total             uint64
	ProposerSlashings uint64
	AttesterSlashings uint64
	Attestations      uint64
	Deposits          uint64
	VoluntaryExits    uint64
	SyncAggregate     uint64
	ExecutionPayload  uint64
}

// Operations is the size of the operation lists and the sync aggregate.
func (sections BlockSections) Operations() uint64 {
	return sections.ProposerSlashings + sections.AttesterSlashings + sections.Attestations +
		sections.Deposits + sections.VoluntaryExits + sections.SyncAggregate
}

// Rest is what is neither an operation nor the payload: the block's header,
// its signature and the body's fixed fields.
func (sections BlockSections) Rest() uint64 {
	return sections.Total - sections.Operations() - sections.ExecutionPayload
}

// BlockSections reads the size of each part of a block's SSZ encoding.
// blockID is as for BlockHeader.
func (client *Client) BlockSections(ctx context.Context, blockID string) (BlockSections, error) {
	path := "/qrl/v1/beacon/blocks/" + url.PathEscape(blockID)
	response, err := client.send(ctx, http.MethodGet, path, http.Header{"Accept": {sszMediaType}}, nil)
	if err != nil {
		return BlockSections{}, err
	}
	defer response.Body.Close()
	if contentType := response.Header.Get("Content-Type"); contentType != sszMediaType {
		return BlockSections{}, fmt.Errorf("GET %s returned %q, not SSZ", path, contentType)
	}
	encoded, err := io.ReadAll(response.Body)
	if err != nil {
		return BlockSections{}, fmt.Errorf("read GET %s: %w", path, err)
	}
	return ParseBlockSections(encoded)
}

// ParseBlockSections reads the offsets of a signed block's SSZ encoding.
func ParseBlockSections(encoded []byte) (BlockSections, error) {
	block, err := variablePart(encoded, 0, signedBlockFixed, "block")
	if err != nil {
		return BlockSections{}, err
	}
	body, err := variablePart(block, blockFixed-4, blockFixed, "block body")
	if err != nil {
		return BlockSections{}, err
	}
	if len(body) < bodyFixed {
		return BlockSections{}, fmt.Errorf("a block body of %d bytes is shorter than its fixed fields", len(body))
	}
	var bounds [bodyOffsets + 1]uint64
	for field := range bodyOffsets {
		bounds[field] = uint64(binary.LittleEndian.Uint32(body[bodyFixedFields+4*field:]))
	}
	bounds[bodyOffsets] = uint64(len(body))
	if bounds[0] != bodyFixed {
		return BlockSections{}, fmt.Errorf("the block body's first offset is %d, not %d", bounds[0], bodyFixed)
	}
	var sizes [bodyOffsets]uint64
	for field := range sizes {
		if bounds[field+1] < bounds[field] {
			return BlockSections{}, fmt.Errorf("the block body's offset %d of %d is past the next, %d", field, bounds[field], bounds[field+1])
		}
		sizes[field] = bounds[field+1] - bounds[field]
	}
	return BlockSections{
		Total:             uint64(len(encoded)),
		ProposerSlashings: sizes[0],
		AttesterSlashings: sizes[1],
		Attestations:      sizes[2],
		Deposits:          sizes[3],
		VoluntaryExits:    sizes[4],
		SyncAggregate:     sizes[5],
		ExecutionPayload:  sizes[6],
	}, nil
}

// variablePart returns what follows the offset at offsetAt of a container
// whose fixed part is fixed bytes long, when that is its only variable field.
func variablePart(container []byte, offsetAt, fixed int, name string) ([]byte, error) {
	if len(container) < fixed {
		return nil, fmt.Errorf("the %s's container of %d bytes is shorter than its fixed part of %d", name, len(container), fixed)
	}
	if offset := binary.LittleEndian.Uint32(container[offsetAt:]); offset != uint32(fixed) {
		return nil, fmt.Errorf("the %s's offset is %d, not %d", name, offset, fixed)
	}
	return container[fixed:], nil
}

// OperationLimits are the most of each operation a block holds.
type OperationLimits struct {
	ProposerSlashings uint64
	AttesterSlashings uint64
	Attestations      uint64
	Deposits          uint64
	VoluntaryExits    uint64
	// CommitteeMembers is the most validators one attestation names.
	CommitteeMembers  uint64
	SyncCommitteeSize uint64
}

// OperationLimits reads the limits from the node's consensus constants.
func (spec Spec) OperationLimits() (OperationLimits, error) {
	var limits OperationLimits
	err := spec.Uints(map[string]*uint64{
		"MAX_PROPOSER_SLASHINGS":       &limits.ProposerSlashings,
		"MAX_ATTESTER_SLASHINGS":       &limits.AttesterSlashings,
		"MAX_ATTESTATIONS":             &limits.Attestations,
		"MAX_DEPOSITS":                 &limits.Deposits,
		"MAX_VOLUNTARY_EXITS":          &limits.VoluntaryExits,
		"MAX_VALIDATORS_PER_COMMITTEE": &limits.CommitteeMembers,
		"SYNC_COMMITTEE_SIZE":          &limits.SyncCommitteeSize,
	})
	return limits, err
}

// Full returns the sections of a block with every operation list at its
// most: each attestation and slashing vote signed by a full committee, and a
// signature of every sync committee member. ML-DSA-87 signatures do not
// aggregate, so each signer adds one. Total and ExecutionPayload are zero.
func (limits OperationLimits) Full() BlockSections {
	members := limits.CommitteeMembers
	indexedAttestation := attestationFixed + members*8 + members*signing.SignatureLength
	// The bitlist's length bit can take one more byte.
	attestation := attestationFixed + members/8 + 1 + members*signing.SignatureLength
	return BlockSections{
		ProposerSlashings: limits.ProposerSlashings * proposerSlashingSize,
		AttesterSlashings: limits.AttesterSlashings * (listOffset + 2*listOffset + 2*indexedAttestation),
		Attestations:      limits.Attestations * (listOffset + attestation),
		Deposits:          limits.Deposits * depositSize,
		VoluntaryExits:    limits.VoluntaryExits * voluntaryExitSize,
		SyncAggregate:     limits.SyncAggregateSize(limits.SyncCommitteeSize),
	}
}

// SyncAggregateSize is the size of a sync aggregate with the given number of
// members' signatures.
func (limits OperationLimits) SyncAggregateSize(participants uint64) uint64 {
	return limits.SyncCommitteeSize/8 + listOffset + participants*signing.SignatureLength
}
