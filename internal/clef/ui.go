// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

package clef

import signercore "github.com/theQRL/go-qrl/signer/core"

type UI struct {
	ApproveTransaction func(*signercore.SignTxRequest) bool
	ApproveData        func(*signercore.SignDataRequest) bool
	Input              func(signercore.UserInputRequest) string
}

func (ui *UI) ApproveTx(request *signercore.SignTxRequest) (signercore.SignTxResponse, error) {
	approved := ui.ApproveTransaction == nil || ui.ApproveTransaction(request)
	return signercore.SignTxResponse{Transaction: request.Transaction, Approved: approved}, nil
}

func (ui *UI) ApproveSignData(request *signercore.SignDataRequest) (signercore.SignDataResponse, error) {
	approved := ui.ApproveData == nil || ui.ApproveData(request)
	return signercore.SignDataResponse{Approved: approved}, nil
}

func (*UI) ApproveListing(request *signercore.ListRequest) (signercore.ListResponse, error) {
	return signercore.ListResponse{Accounts: request.Accounts}, nil
}

func (*UI) ApproveNewAccount(*signercore.NewAccountRequest) (signercore.NewAccountResponse, error) {
	return signercore.NewAccountResponse{Approved: true}, nil
}

func (*UI) ShowError(signercore.Message) {}

func (*UI) ShowInfo(signercore.Message) {}

func (*UI) OnApprovedTx(any) {}

func (*UI) OnSignerStartup(signercore.StartupInfo) {}

func (ui *UI) OnInputRequired(request signercore.UserInputRequest) (signercore.UserInputResponse, error) {
	var input string
	if ui.Input != nil {
		input = ui.Input(request)
	}
	return signercore.UserInputResponse{Text: input}, nil
}
