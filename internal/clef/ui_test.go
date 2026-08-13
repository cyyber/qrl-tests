package clef

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/accounts"
	signercore "github.com/theQRL/go-qrl/signer/core"
)

func TestUIApprovesAccountListing(t *testing.T) {
	listed := []accounts.Account{{}}
	response, err := (&UI{}).ApproveListing(&signercore.ListRequest{Accounts: listed})
	require.NoError(t, err)
	require.Equal(t, listed, response.Accounts)
}

func TestUIAnswersPasswordRequests(t *testing.T) {
	response, err := (&UI{Password: "runtime-password"}).OnInputRequired(signercore.UserInputRequest{})
	require.NoError(t, err)
	require.Equal(t, "runtime-password", response.Text)
}
