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
}

// AttestationData is what an attestation votes for.
type AttestationData struct {
	Slot            uint64     `json:"slot,string"`
	CommitteeIndex  uint64     `json:"index,string"`
	BeaconBlockRoot string     `json:"beacon_block_root"`
	Source          Checkpoint `json:"source"`
	Target          Checkpoint `json:"target"`
}

// Attestation is an aggregate a block includes: one vote of the committee
// members its aggregation bits set.
type Attestation struct {
	AggregationBits []byte
	Data            AttestationData
}

// Includes reports whether the committee member at position voted in the
// attestation. The aggregation bits are an SSZ bitlist, whose highest set bit
// marks its length.
func (attestation Attestation) Includes(position uint64) bool {
	aggregation := attestation.AggregationBits
	var length uint64
	for index := len(aggregation) - 1; index >= 0; index-- {
		if aggregation[index] != 0 {
			length = uint64(index)*8 + uint64(bits.Len8(aggregation[index])) - 1
			break
		}
	}
	return position < length && aggregation[position/8]&(1<<(position%8)) != 0
}

// Committee is the validators that attest together at one slot, in the
// order their aggregation bits follow.
type Committee struct {
	Index      uint64
	Slot       uint64
	Validators []uint64
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
	Total uint64 `json:"total,string"`
}
