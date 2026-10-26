package beacon

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

func (client *Client) Genesis(ctx context.Context) (Genesis, error) {
	return getData[Genesis](ctx, client, "/qrl/v1/beacon/genesis")
}

func (client *Client) DepositContract(ctx context.Context) (DepositContract, error) {
	return getData[DepositContract](ctx, client, "/qrl/v1/config/deposit_contract")
}

func (client *Client) SpecUint(ctx context.Context, name string) (uint64, error) {
	values, err := getData[map[string]string](ctx, client, "/qrl/v1/config/spec")
	if err != nil {
		return 0, err
	}
	value, ok := values[name]
	if !ok {
		return 0, fmt.Errorf("consensus spec does not define %s", name)
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse spec value %s=%q: %w", name, value, err)
	}
	return parsed, nil
}

// SyncStatus reports how far the node is from the head of the chain.
func (client *Client) SyncStatus(ctx context.Context) (SyncStatus, error) {
	return getData[SyncStatus](ctx, client, "/qrl/v1/node/syncing")
}

// Identity returns the node's libp2p peer ID and the addresses it listens on.
func (client *Client) Identity(ctx context.Context) (Identity, error) {
	return getData[Identity](ctx, client, "/qrl/v1/node/identity")
}

func (client *Client) HeadSlot(ctx context.Context) (uint64, error) {
	head, err := client.BlockHeader(ctx, "head")
	return head.Slot, err
}

// BlockHeader returns the slot and root of a canonical block. blockID is
// "head", "finalized", a slot or a 0x-prefixed root. A slot without a block
// is reported through IsNotFound.
func (client *Client) BlockHeader(ctx context.Context, blockID string) (BlockHeader, error) {
	header, err := getData[struct {
		Root   string `json:"root"`
		Header struct {
			Message struct {
				Slot uint64 `json:"slot,string"`
			} `json:"message"`
		} `json:"header"`
	}](ctx, client, "/qrl/v1/beacon/headers/"+url.PathEscape(blockID))
	return BlockHeader{Slot: header.Header.Message.Slot, Root: header.Root}, err
}

// FinalityCheckpoints returns the justified and finalized checkpoints of the
// head state.
func (client *Client) FinalityCheckpoints(ctx context.Context) (FinalityCheckpoints, error) {
	return client.FinalityCheckpointsAt(ctx, "head")
}

// FinalityCheckpointsAt returns the justified and finalized checkpoints of
// the state at stateID, as ValidatorAt takes it.
func (client *Client) FinalityCheckpointsAt(ctx context.Context, stateID string) (FinalityCheckpoints, error) {
	return getData[FinalityCheckpoints](
		ctx, client, "/qrl/v1/beacon/states/"+url.PathEscape(stateID)+"/finality_checkpoints",
	)
}

// SlashedValidators returns the validators of the head state that have been
// slashed.
func (client *Client) SlashedValidators(ctx context.Context) ([]Validator, error) {
	records, err := getData[[]validatorWire](
		ctx, client, "/qrl/v1/beacon/states/head/validators?status=active_slashed&status=exited_slashed",
	)
	if err != nil {
		return nil, err
	}
	validators := make([]Validator, len(records))
	for index, record := range records {
		validators[index] = record.validator()
	}
	return validators, nil
}

type validatorWire struct {
	Index     uint64 `json:"index,string"`
	Balance   uint64 `json:"balance,string"`
	Status    string `json:"status"`
	Validator struct {
		PublicKey           string `json:"pubkey"`
		WithdrawalRecipient string `json:"withdrawal_recipient"`
		RandaoCommitment    string `json:"randao_commitment"`
		EffectiveBalance    uint64 `json:"effective_balance,string"`
		Slashed             bool   `json:"slashed"`
		ActivationEpoch     uint64 `json:"activation_epoch,string"`
		ExitEpoch           uint64 `json:"exit_epoch,string"`
		WithdrawableEpoch   uint64 `json:"withdrawable_epoch,string"`
	} `json:"validator"`
}

// Validator looks a validator up by index or 0x-prefixed public key in the
// head state. An unknown validator is reported through IsNotFound.
func (client *Client) Validator(ctx context.Context, validatorID string) (Validator, error) {
	return client.ValidatorAt(ctx, "head", validatorID)
}

// ValidatorAt looks a validator up in the state at stateID, which is "head",
// "finalized" or a slot. The state at a slot is the one after its block, or
// after the empty slot.
func (client *Client) ValidatorAt(ctx context.Context, stateID, validatorID string) (Validator, error) {
	record, err := getData[validatorWire](
		ctx, client, "/qrl/v1/beacon/states/"+url.PathEscape(stateID)+"/validators/"+url.PathEscape(validatorID),
	)
	if err != nil {
		return Validator{}, err
	}
	return record.validator(), nil
}

// TotalActiveBalance sums the effective balances of the validators active in
// the epoch of the state at stateID, slashed ones included, as the base
// reward of that state's epoch processing does. It reads every active
// validator, which suits a devnet, not a large network.
func (client *Client) TotalActiveBalance(ctx context.Context, stateID string) (uint64, error) {
	records, err := getData[[]validatorWire](
		ctx, client,
		"/qrl/v1/beacon/states/"+url.PathEscape(stateID)+
			"/validators?status=active_ongoing&status=active_exiting&status=active_slashed",
	)
	if err != nil {
		return 0, err
	}
	var total uint64
	for _, record := range records {
		total += record.Validator.EffectiveBalance
	}
	return total, nil
}

func (record validatorWire) validator() Validator {
	return Validator{
		Index:               record.Index,
		Balance:             record.Balance,
		Status:              record.Status,
		PublicKey:           record.Validator.PublicKey,
		WithdrawalRecipient: record.Validator.WithdrawalRecipient,
		RandaoCommitment:    record.Validator.RandaoCommitment,
		EffectiveBalance:    record.Validator.EffectiveBalance,
		Slashed:             record.Validator.Slashed,
		ActivationEpoch:     record.Validator.ActivationEpoch,
		ExitEpoch:           record.Validator.ExitEpoch,
		WithdrawableEpoch:   record.Validator.WithdrawableEpoch,
	}
}

type blockWire struct {
	Message struct {
		Slot uint64 `json:"slot,string"`
		Body struct {
			VoluntaryExits []SignedVoluntaryExit `json:"voluntary_exits"`
			Payload        struct {
				BlockNumber uint64       `json:"block_number,string"`
				Withdrawals []Withdrawal `json:"withdrawals"`
			} `json:"execution_payload"`
		} `json:"body"`
	} `json:"message"`
}

// BlockOperations returns the exits and withdrawals in one block. A slot
// without a block is reported through IsNotFound.
func (client *Client) BlockOperations(ctx context.Context, blockID string) (BlockOperations, error) {
	block, err := getData[blockWire](ctx, client, "/qrl/v1/beacon/blocks/"+url.PathEscape(blockID))
	if err != nil {
		return BlockOperations{}, err
	}

	operations := BlockOperations{
		Slot:                 block.Message.Slot,
		ExecutionBlockNumber: block.Message.Body.Payload.BlockNumber,
		Withdrawals:          block.Message.Body.Payload.Withdrawals,
	}
	for _, exit := range block.Message.Body.VoluntaryExits {
		operations.VoluntaryExits = append(operations.VoluntaryExits, exit.Message.ValidatorIndex)
	}
	return operations, nil
}

func (client *Client) AttesterDuties(ctx context.Context, epoch uint64, indices []uint64) ([]AttesterDuty, error) {
	path := "/qrl/v1/validator/duties/attester/" + strconv.FormatUint(epoch, 10)
	return postData[[]AttesterDuty](ctx, client, path, formatIndices(indices))
}

func (client *Client) SubmitVoluntaryExit(ctx context.Context, exit SignedVoluntaryExit) error {
	return client.postJSON(ctx, "/qrl/v1/beacon/pool/voluntary_exits", exit, nil)
}

// AttestationRewards returns the given validators' attestation rewards for a
// completed epoch: their head, source and target rewards in shor, negative
// for a penalty. Qrysm serves an epoch only after two later epochs have
// elapsed so every attestation has a chance of inclusion.
func (client *Client) AttestationRewards(ctx context.Context, epoch uint64, indices []uint64) ([]AttestationReward, error) {
	path := "/qrl/v1/beacon/rewards/attestations/" + strconv.FormatUint(epoch, 10)
	rewards, err := postData[struct {
		TotalRewards []AttestationReward `json:"total_rewards"`
	}](ctx, client, path, formatIndices(indices))
	return rewards.TotalRewards, err
}

// SyncCommitteeRewards returns what the sync aggregate of one block paid the
// given validators, negative for a missed duty. A validator outside the sync
// committee is left out.
func (client *Client) SyncCommitteeRewards(ctx context.Context, blockID string, indices []uint64) ([]SyncCommitteeReward, error) {
	path := "/qrl/v1/beacon/rewards/sync_committee/" + url.PathEscape(blockID)
	return postData[[]SyncCommitteeReward](ctx, client, path, formatIndices(indices))
}

func formatIndices(indices []uint64) []string {
	formatted := make([]string, len(indices))
	for position, index := range indices {
		formatted[position] = strconv.FormatUint(index, 10)
	}
	return formatted
}
