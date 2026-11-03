package remotesigner

// The artifact types of go-remotesigner's docs/contract.md, section
// "Artifact types", the values of a sign request's type.
// Every uint64 travels as a decimal string and every byte string as
// 0x-prefixed hex. A request without a signing root lets the signer compute
// it from its own network.
const (
	TypeBlockV2                           = "BLOCK_V2"
	TypeAttestation                       = "ATTESTATION"
	TypeAggregationSlot                   = "AGGREGATION_SLOT"
	TypeAggregateAndProof                 = "AGGREGATE_AND_PROOF"
	TypeVoluntaryExit                     = "VOLUNTARY_EXIT"
	TypeSyncCommitteeMessage              = "SYNC_COMMITTEE_MESSAGE"
	TypeSyncCommitteeSelectionProof       = "SYNC_COMMITTEE_SELECTION_PROOF"
	TypeSyncCommitteeContributionAndProof = "SYNC_COMMITTEE_CONTRIBUTION_AND_PROOF"
	TypeValidatorRegistration             = "VALIDATOR_REGISTRATION"

	// blockVersion is the only block version the target stack has.
	blockVersion = "ZOND"
)

// Reasons of the 412 refusals of slashing protection.
const (
	ReasonDoubleVote     = "SLASHING_DOUBLE_VOTE"
	ReasonDoubleProposal = "SLASHING_DOUBLE_PROPOSAL"
)

type SignRequest struct {
	Type        string           `json:"type"`
	ForkInfo    ForkInfo         `json:"fork_info"`
	Attestation *AttestationData `json:"attestation,omitempty"`
	BeaconBlock *BeaconBlockV2   `json:"beacon_block,omitempty"`
}

type ForkInfo struct {
	Fork                  Fork   `json:"fork"`
	GenesisValidatorsRoot string `json:"genesis_validators_root"`
}

type Fork struct {
	PreviousVersion string `json:"previous_version"`
	CurrentVersion  string `json:"current_version"`
	Epoch           uint64 `json:"epoch,string"`
}

// GenesisForkInfo is the fork info of every request on a chain with a single
// fork, as the qrysm client sends it.
func GenesisForkInfo(genesisForkVersion, genesisValidatorsRoot string) ForkInfo {
	return ForkInfo{
		Fork:                  Fork{PreviousVersion: genesisForkVersion, CurrentVersion: genesisForkVersion},
		GenesisValidatorsRoot: genesisValidatorsRoot,
	}
}

type Checkpoint struct {
	Epoch uint64 `json:"epoch,string"`
	Root  string `json:"root"`
}

// AttestationData is SSZ AttestationData; Index is the committee index.
type AttestationData struct {
	Slot            uint64     `json:"slot,string"`
	Index           uint64     `json:"index,string"`
	BeaconBlockRoot string     `json:"beacon_block_root"`
	Source          Checkpoint `json:"source"`
	Target          Checkpoint `json:"target"`
}

type BeaconBlockHeader struct {
	Slot          uint64 `json:"slot,string"`
	ProposerIndex uint64 `json:"proposer_index,string"`
	ParentRoot    string `json:"parent_root"`
	StateRoot     string `json:"state_root"`
	BodyRoot      string `json:"body_root"`
}

// BeaconBlockV2 carries only the block's header, whose root is the block's.
type BeaconBlockV2 struct {
	Version     string            `json:"version"`
	BlockHeader BeaconBlockHeader `json:"block_header"`
}

// AttestationRequest asks for an attestation signature, which slashing
// protection checks against the key's signed attestations.
func AttestationRequest(forkInfo ForkInfo, data AttestationData) SignRequest {
	return SignRequest{Type: TypeAttestation, ForkInfo: forkInfo, Attestation: &data}
}

// BlockRequest asks for a block signature, which slashing protection checks
// against the key's signed blocks.
func BlockRequest(forkInfo ForkInfo, header BeaconBlockHeader) SignRequest {
	return SignRequest{
		Type:        TypeBlockV2,
		ForkInfo:    forkInfo,
		BeaconBlock: &BeaconBlockV2{Version: blockVersion, BlockHeader: header},
	}
}
