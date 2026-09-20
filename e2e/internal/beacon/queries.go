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

func (client *Client) Fork(ctx context.Context) (Fork, error) {
	return getData[Fork](ctx, client, "/qrl/v1/beacon/states/head/fork")
}

func (client *Client) DepositContract(ctx context.Context) (DepositContract, error) {
	return getData[DepositContract](ctx, client, "/qrl/v1/config/deposit_contract")
}

// SpecUint reads one numeric value from the chain spec.
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

func (client *Client) HeadSlot(ctx context.Context) (uint64, error) {
	head, err := getData[struct {
		Header struct {
			Message struct {
				Slot uint64 `json:"slot,string"`
			} `json:"message"`
		} `json:"header"`
	}](ctx, client, "/qrl/v1/beacon/headers/head")
	return head.Header.Message.Slot, err
}

type validatorContainerWire struct {
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
	record, err := getData[validatorContainerWire](
		ctx, client, "/qrl/v1/beacon/states/head/validators/"+url.PathEscape(validatorID),
	)
	if err != nil {
		return Validator{}, err
	}
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
	}, nil
}

type blockWire struct {
	Message struct {
		Slot uint64 `json:"slot,string"`
		Body struct {
			VoluntaryExits []SignedVoluntaryExit `json:"voluntary_exits"`
			Payload        struct {
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

	operations := BlockOperations{Slot: block.Message.Slot, Withdrawals: block.Message.Body.Payload.Withdrawals}
	for _, exit := range block.Message.Body.VoluntaryExits {
		operations.VoluntaryExits = append(operations.VoluntaryExits, exit.Message.ValidatorIndex)
	}
	return operations, nil
}

// AttesterDuties returns the attestation assignments of the given validators
// for an epoch.
func (client *Client) AttesterDuties(ctx context.Context, epoch uint64, indices []uint64) ([]AttesterDuty, error) {
	path := "/qrl/v1/validator/duties/attester/" + strconv.FormatUint(epoch, 10)
	return postData[[]AttesterDuty](ctx, client, path, formatIndices(indices))
}

func (client *Client) SubmitVoluntaryExit(ctx context.Context, exit SignedVoluntaryExit) error {
	return client.postJSON(ctx, "/qrl/v1/beacon/pool/voluntary_exits", exit, nil)
}

// AttestationRewards returns the attestation scores for the given validators
// in a completed epoch. Qrysm serves an epoch only after two later epochs
// have elapsed so every attestation has a chance of inclusion.
func (client *Client) AttestationRewards(ctx context.Context, epoch uint64, indices []uint64) ([]AttestationReward, error) {
	path := "/qrl/v1/beacon/rewards/attestations/" + strconv.FormatUint(epoch, 10)
	rewards, err := postData[struct {
		TotalRewards []AttestationReward `json:"total_rewards"`
	}](ctx, client, path, formatIndices(indices))
	return rewards.TotalRewards, err
}

// formatIndices renders validator indices the way the beacon API expects
// them in request bodies: as decimal strings.
func formatIndices(indices []uint64) []string {
	formatted := make([]string, len(indices))
	for position, index := range indices {
		formatted[position] = strconv.FormatUint(index, 10)
	}
	return formatted
}
