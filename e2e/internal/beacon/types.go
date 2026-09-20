package beacon

// FarFutureEpoch is the sentinel the beacon API reports for epochs that have
// not been scheduled, such as the exit epoch of an active validator.
const FarFutureEpoch = ^uint64(0)

type Genesis struct {
	Time           uint64 `json:"genesis_time,string"`
	ValidatorsRoot string `json:"genesis_validators_root"`
	ForkVersion    string `json:"genesis_fork_version"`
}

type Fork struct {
	PreviousVersion string `json:"previous_version"`
	CurrentVersion  string `json:"current_version"`
	Epoch           uint64 `json:"epoch,string"`
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
	Slot           uint64
	VoluntaryExits []uint64
	Withdrawals    []Withdrawal
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
