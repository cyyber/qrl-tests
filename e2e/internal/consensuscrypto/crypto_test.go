package consensuscrypto

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

// The expected values were produced by qrysm's SSZ generated code and
// beacon-chain/core/signing for the same inputs.

func TestDepositRootsMatchQrysm(t *testing.T) {
	publicKey := bytes.Repeat([]byte{0x11}, PublicKeyLength)
	recipient := bytes.Repeat([]byte{0x22}, WithdrawalRecipientLength)
	commitment := bytes.Repeat([]byte{0x33}, RandaoCommitmentLength)
	signature := bytes.Repeat([]byte{0x44}, SignatureLength)

	message := DepositMessage{
		PublicKey: publicKey, WithdrawalRecipient: recipient, Amount: 40000000000000, RandaoCommitment: commitment,
	}
	messageRoot, err := message.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, "d957a6404c9eca4e40f8de7005a25ad4b28467375aa328d889daff3d62909adf", hex.EncodeToString(messageRoot[:]))

	dataRoot, err := DepositData{DepositMessage: message, Signature: signature}.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, "aebc87a70fe917788ede6ee25d3e3e453a42b856b46e54ccc73b67d9d95b3835", hex.EncodeToString(dataRoot[:]))
}

func TestDepositRootsRejectWrongLengths(t *testing.T) {
	_, err := DepositMessage{PublicKey: []byte{1}}.HashTreeRoot()
	require.ErrorContains(t, err, "deposit public key must be 2592 bytes")
}

func TestVoluntaryExitSigningRootMatchesQrysm(t *testing.T) {
	exitRoot, err := VoluntaryExit{Epoch: 12, ValidatorIndex: 64}.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, "d2e59fb9a42ce1736c3619e1b7aaf5d561bf8326eb079fdce3f9d24acb0eda4a", hex.EncodeToString(exitRoot[:]))

	var genesisRoot [RootLength]byte
	copy(genesisRoot[:], bytes.Repeat([]byte{0x55}, RootLength))
	domain := ComputeDomain(DomainVoluntaryExit, [4]byte{0x10, 0x00, 0x00, 0x20}, genesisRoot)
	require.Equal(t, "040000008a56f5ef2f9a899359789c0a048f96d5d1e1e3446b2cc5e02d1a6e78", hex.EncodeToString(domain[:]))

	signingRoot := SigningRoot(exitRoot, domain)
	require.Equal(t, "745a91a7ed70c334e37d38af4a2ca0a66d7a24896492ec37df054131e373516c", hex.EncodeToString(signingRoot[:]))
}

func TestDepositDomainUsesZeroGenesisRoot(t *testing.T) {
	domain := ComputeDomain(DomainDeposit, [4]byte{0x10, 0x00, 0x00, 0x20}, [RootLength]byte{})
	require.Equal(t, "0300000014a245e19985c526271213b92ac915999e4bd92e4c4a3c05e19f7557", hex.EncodeToString(domain[:]))
}

func TestRandaoCommitmentMatchesQrysm(t *testing.T) {
	seed := make([]byte, 48)
	for index := range seed {
		seed[index] = byte(0x91 + index)
	}
	commitment := RandaoCommitment(seed)
	require.Equal(t, "3a4c6ce43d45814c5f37ef3d08b4888af27ffbfb8f2870d0229d3fabe941c6b9", hex.EncodeToString(commitment[:]))
}
