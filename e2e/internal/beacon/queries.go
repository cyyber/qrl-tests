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

func (client *Client) Head(ctx context.Context) (Head, error) {
	var response struct {
		Data struct {
			Root   string `json:"root"`
			Header struct {
				Message struct {
					Slot uint64 `json:"slot,string"`
				} `json:"message"`
			} `json:"header"`
		} `json:"data"`
	}
	if err := client.do(ctx, "GET", "/qrl/v1/beacon/headers/head", nil, &response); err != nil {
		return Head{}, err
	}
	return Head{Slot: response.Data.Header.Message.Slot, Root: response.Data.Root}, nil
}

func (client *Client) HeadSlot(ctx context.Context) (uint64, error) {
	head, err := client.Head(ctx)
	return head.Slot, err
}

func (client *Client) FinalizedCheckpoint(ctx context.Context) (Checkpoint, error) {
	data, err := getData[struct {
		Finalized Checkpoint `json:"finalized"`
	}](ctx, client, "/qrl/v1/beacon/states/head/finality_checkpoints")
	if err != nil {
		return Checkpoint{}, err
	}
	return data.Finalized, nil
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
	request := make([]string, len(indices))
	for position, index := range indices {
		request[position] = strconv.FormatUint(index, 10)
	}
	path := "/qrl/v1/validator/duties/attester/" + strconv.FormatUint(epoch, 10)
	return postData[[]AttesterDuty](ctx, client, path, request)
}

func (client *Client) SubmitVoluntaryExit(ctx context.Context, exit SignedVoluntaryExit) error {
	return client.Post(ctx, "/qrl/v1/beacon/pool/voluntary_exits", exit)
}
