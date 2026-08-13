package clef

import signercore "github.com/theQRL/go-qrl/signer/core"

type UI struct {
	Password string
}

func (*UI) ApproveTx(request *signercore.SignTxRequest) (signercore.SignTxResponse, error) {
	return signercore.SignTxResponse{Transaction: request.Transaction, Approved: true}, nil
}

func (*UI) ApproveSignData(*signercore.SignDataRequest) (signercore.SignDataResponse, error) {
	return signercore.SignDataResponse{Approved: true}, nil
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

func (ui *UI) OnInputRequired(signercore.UserInputRequest) (signercore.UserInputResponse, error) {
	return signercore.UserInputResponse{Text: ui.Password}, nil
}
