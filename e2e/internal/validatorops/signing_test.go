package validatorops

import (
	"context"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/consensuscontext"
	"github.com/cyyber/qrl-tests/e2e/internal/consensuscrypto"
	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/common/hexutil"
)

var signingGenesisRoot = [consensuscrypto.RootLength]byte{0x55}

type signingSource struct{}

func (signingSource) Genesis(context.Context) (beacon.Genesis, error) {
	return beacon.Genesis{
		ValidatorsRoot: hexutil.Encode(signingGenesisRoot[:]),
		ForkVersion:    "0x10000020",
	}, nil
}

func (signingSource) Fork(context.Context) (beacon.Fork, error) {
	return beacon.Fork{PreviousVersion: "0x10000021", CurrentVersion: "0x10000022", Epoch: 12}, nil
}

func (signingSource) SpecUint(context.Context, string) (uint64, error) {
	return 8, nil
}

func loadSigningContext(t *testing.T) consensuscontext.Context {
	t.Helper()
	chain, err := consensuscontext.Load(t.Context(), signingSource{})
	require.NoError(t, err)
	return chain
}

func TestConsensusSigningDomains(t *testing.T) {
	chain := loadSigningContext(t)
	for _, test := range []struct {
		slot    uint64
		version [4]byte
	}{
		{95, [4]byte{0x10, 0, 0, 0x21}},
		{96, [4]byte{0x10, 0, 0, 0x22}},
		{104, [4]byte{0x10, 0, 0, 0x22}},
	} {
		want := consensuscrypto.ComputeDomain(consensuscrypto.DomainVoluntaryExit, test.version, signingGenesisRoot)
		require.Equal(t, want, chain.Domain(consensuscrypto.DomainVoluntaryExit, chain.Epoch(test.slot)), "slot %d", test.slot)
	}
	// Deposits use the genesis version and a zero root even after a fork.
	wantDeposit := consensuscrypto.ComputeDomain(consensuscrypto.DomainDeposit, [4]byte{0x10, 0, 0, 0x20}, [consensuscrypto.RootLength]byte{})
	require.Equal(t, wantDeposit, chain.DepositDomain())
}

func TestDepositInputSignsRequestedData(t *testing.T) {
	chain := loadSigningContext(t)
	key, err := DeterministicKey(0x91)
	require.NoError(t, err)
	recipientKey, err := DeterministicKey(0x92)
	require.NoError(t, err)
	recipient := recipientKey.Address()
	const amount = uint64(20000000000000)

	data, root, err := depositInput(key, recipient, amount, chain.DepositDomain())
	require.NoError(t, err)
	expected := consensuscrypto.DepositData{
		PublicKey: key.PublicKey(), WithdrawalRecipient: recipient.Bytes(), Amount: amount,
		RandaoCommitment: key.RandaoCommitment(), Signature: data.Signature,
	}
	require.Equal(t, expected, data)
	expectedRoot, err := expected.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, expectedRoot, root)

	messageRoot, err := (consensuscrypto.DepositMessage{
		PublicKey: expected.PublicKey, WithdrawalRecipient: recipient.Bytes(), Amount: amount,
		RandaoCommitment: expected.RandaoCommitment,
	}).HashTreeRoot()
	require.NoError(t, err)
	domain := consensuscrypto.ComputeDomain(consensuscrypto.DomainDeposit, [4]byte{0x10, 0, 0, 0x20}, [consensuscrypto.RootLength]byte{})
	require.NoError(t, consensuscrypto.Verify(consensuscrypto.SigningRoot(messageRoot, domain), key.PublicKey(), data.Signature))
}

func TestVoluntaryExitSignsRequestedData(t *testing.T) {
	chain := loadSigningContext(t)
	key, err := DeterministicKey(0x91)
	require.NoError(t, err)
	for _, test := range []struct {
		epoch   uint64
		version [4]byte
	}{
		{11, [4]byte{0x10, 0, 0, 0x21}},
		{12, [4]byte{0x10, 0, 0, 0x22}},
	} {
		exit, err := VoluntaryExit(key, 64, test.epoch, chain)
		require.NoError(t, err)
		require.Equal(t, beacon.VoluntaryExit{Epoch: test.epoch, ValidatorIndex: 64}, exit.Message)
		signature, err := hexutil.Decode(exit.Signature)
		require.NoError(t, err)
		root, err := (consensuscrypto.VoluntaryExit{Epoch: test.epoch, ValidatorIndex: 64}).HashTreeRoot()
		require.NoError(t, err)
		domain := consensuscrypto.ComputeDomain(consensuscrypto.DomainVoluntaryExit, test.version, signingGenesisRoot)
		require.NoError(t, consensuscrypto.Verify(consensuscrypto.SigningRoot(root, domain), key.PublicKey(), signature), "epoch %d", test.epoch)
	}
}
