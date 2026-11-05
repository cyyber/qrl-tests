package beacon

import "math/bits"

// FarFutureEpoch is the sentinel the beacon API reports for epochs that have
// not been scheduled, such as the exit epoch of an active validator.
const FarFutureEpoch = ^uint64(0)

// Spec holds the consensus constants a node runs, by name, as the beacon API
// reports them.
type Spec map[string]string

type Genesis struct {
	Time           uint64 `json:"genesis_time,string"`
	ValidatorsRoot string `json:"genesis_validators_root"`
	ForkVersion    string `json:"genesis_fork_version"`
}

type BlockHeader struct {
	Slot uint64
	Root string
}

type SyncStatus struct {
	HeadSlot     uint64 `json:"head_slot,string"`
	SyncDistance uint64 `json:"sync_distance,string"`
	IsSyncing    bool   `json:"is_syncing"`
	IsOptimistic bool   `json:"is_optimistic"`
	ELOffline    bool   `json:"el_offline"`
}

type Identity struct {
	PeerID       string   `json:"peer_id"`
	P2PAddresses []string `json:"p2p_addresses"`
}

type Checkpoint struct {
	Epoch uint64 `json:"epoch,string"`
	Root  string `json:"root"`
}

type FinalityCheckpoints struct {
	PreviousJustified Checkpoint `json:"previous_justified"`
	CurrentJustified  Checkpoint `json:"current_justified"`
	Finalized         Checkpoint `json:"finalized"`
}

type DepositContract struct {
	ChainID uint64 `json:"chain_id,string"`
	Address string `json:"address"`
}

type Validator struct {
	Index               uint64
	Balance             uint64
	Status              string
	PublicKey           string
	WithdrawalRecipient string
	RandaoCommitment    string
	EffectiveBalance    uint64
	Slashed             bool
	ActivationEpoch     uint64
	ExitEpoch           uint64
	WithdrawableEpoch   uint64
}

type VoluntaryExit struct {
	Epoch          uint64 `json:"epoch,string"`
	ValidatorIndex uint64 `json:"validator_index,string"`
}

type SignedVoluntaryExit struct {
	Message   VoluntaryExit `json:"message"`
	Signature string        `json:"signature"`
}

type Withdrawal struct {
	ValidatorIndex uint64 `json:"validator_index,string"`
	Address        string `json:"address"`
	Amount         uint64 `json:"amount,string"`
}

type BlockOperations struct {
	Slot                 uint64
	ProposerIndex        uint64
	ExecutionBlockNumber uint64
	VoluntaryExits       []uint64
	Withdrawals          []Withdrawal
	Attestations         []Attestation
	ProposerSlashings    []ProposerSlashing
	AttesterSlashings    []AttesterSlashing
}

// AttestationData is what an attestation votes for.
type AttestationData struct {
	Slot            uint64     `json:"slot,string"`
	CommitteeIndex  uint64     `json:"index,string"`
	BeaconBlockRoot string     `json:"beacon_block_root"`
	Source          Checkpoint `json:"source"`
	Target          Checkpoint `json:"target"`
}

// Attestation is a vote that a block includes. Its aggregation bits mark the
// committee members who cast it.
type Attestation struct {
	AggregationBits []byte
	Data            AttestationData
}

// Includes reports whether the committee member at position voted.
func (attestation Attestation) Includes(position uint64) bool {
	aggregation := attestation.AggregationBits
	return position < bitlistLen(aggregation) && aggregation[position/8]&(1<<(position%8)) != 0
}

// bitlistLen returns the length of an SSZ bitlist: the index of its highest
// set bit, which is in the last byte.
func bitlistLen(bitlist []byte) uint64 {
	if len(bitlist) == 0 || bitlist[len(bitlist)-1] == 0 {
		return 0
	}
	return uint64(len(bitlist)-1)*8 + uint64(bits.Len8(bitlist[len(bitlist)-1])) - 1
}

// Committee is the validators that attest at one slot, in aggregation bit
// order.
type Committee struct {
	Index      uint64
	Slot       uint64
	Validators []uint64
}

// IndexedAttestation is an attestation with its attesters named by validator
// index, as slashing evidence carries it.
type IndexedAttestation struct {
	AttestingIndices []uint64
	Data             AttestationData
}

// AttesterSlashing is the evidence of a double or surround vote: two
// conflicting attestations. Every validator that signed both is slashed.
type AttesterSlashing struct {
	Attestation1 IndexedAttestation
	Attestation2 IndexedAttestation
}

type BeaconBlockHeaderMessage struct {
	Slot          uint64 `json:"slot,string"`
	ProposerIndex uint64 `json:"proposer_index,string"`
	ParentRoot    string `json:"parent_root"`
	StateRoot     string `json:"state_root"`
	BodyRoot      string `json:"body_root"`
}

// ProposerSlashing is the evidence of a double proposal: two headers of one
// slot that the proposer signed.
type ProposerSlashing struct {
	Header1 BeaconBlockHeaderMessage
	Header2 BeaconBlockHeaderMessage
}

type ProposerDuty struct {
	PublicKey      string `json:"pubkey"`
	ValidatorIndex uint64 `json:"validator_index,string"`
	Slot           uint64 `json:"slot,string"`
}

type AttesterDuty struct {
	PublicKey      string `json:"pubkey"`
	ValidatorIndex uint64 `json:"validator_index,string"`
	Slot           uint64 `json:"slot,string"`
}

type AttestationReward struct {
	ValidatorIndex uint64 `json:"validator_index,string"`
	Head           int64  `json:"head,string"`
	Target         int64  `json:"target,string"`
	Source         int64  `json:"source,string"`
}

type SyncCommitteeReward struct {
	ValidatorIndex uint64 `json:"validator_index,string"`
	Reward         int64  `json:"reward,string"`
}

type IdealAttestationReward struct {
	EffectiveBalance uint64 `json:"effective_balance,string"`
	Head             int64  `json:"head,string"`
	Target           int64  `json:"target,string"`
	Source           int64  `json:"source,string"`
}

type BlockRewards struct {
	Total             uint64 `json:"total,string"`
	ProposerSlashings uint64 `json:"proposer_slashings,string"`
	AttesterSlashings uint64 `json:"attester_slashings,string"`
}
