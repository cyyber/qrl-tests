package signing

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrllib/wallet/common"
	walletmldsa "github.com/theQRL/go-qrllib/wallet/ml_dsa_87"
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

func TestDepositDomainMatchesQrysm(t *testing.T) {
	domain := ComputeDomain(DomainDeposit, ForkVersion{0x10, 0x00, 0x00, 0x20}, Root{})
	require.Equal(t, "0300000014a245e19985c526271213b92ac915999e4bd92e4c4a3c05e19f7557", hex.EncodeToString(domain[:]))
}

func repeatedRoot(value byte) Root {
	return Root(bytes.Repeat([]byte{value}, RootLength))
}

func TestBeaconBlockHeaderRootMatchesQrysm(t *testing.T) {
	header := BeaconBlockHeader{
		Slot: 123, ProposerIndex: 45,
		ParentRoot: repeatedRoot(0x11), StateRoot: repeatedRoot(0x22), BodyRoot: repeatedRoot(0x33),
	}
	root := header.HashTreeRoot()
	require.Equal(t, "43d88b69660aef4a5898e981591de4f5114841ab7e04c312212bbb8d0fe0c85a", hex.EncodeToString(root[:]))

	fork := Fork{CurrentVersion: ForkVersion{0x10, 0x00, 0x00, 0x20}}
	signingRoot := SigningRoot(root, fork.Domain(DomainBeaconProposer, 3, repeatedRoot(0x77)))
	require.Equal(t, "b08ecc69319a6b725dd53b0469e8a80c4db96b9239e6c1ee5b5e3d18dcf74db7", hex.EncodeToString(signingRoot[:]))
}

func TestForkDomainKeepsThePreviousVersionBeforeTheFork(t *testing.T) {
	fork := Fork{PreviousVersion: ForkVersion{0x01}, CurrentVersion: ForkVersion{0x02}, Epoch: 10}
	genesisValidatorsRoot := repeatedRoot(0x77)
	require.Equal(t, ComputeDomain(DomainBeaconProposer, ForkVersion{0x01}, genesisValidatorsRoot),
		fork.Domain(DomainBeaconProposer, 9, genesisValidatorsRoot))
	require.Equal(t, ComputeDomain(DomainBeaconProposer, ForkVersion{0x02}, genesisValidatorsRoot),
		fork.Domain(DomainBeaconProposer, 10, genesisValidatorsRoot))
	domain := ComputeDomain(DomainBeaconProposer, ForkVersion{0x10, 0x00, 0x00, 0x20}, genesisValidatorsRoot)
	require.Equal(t, "000000007c84f6ac711e7d87eadc076350fd55347d33483c0005bd0867748499", hex.EncodeToString(domain[:]))
}

func TestParseHex(t *testing.T) {
	root, err := ParseRoot("0x" + hex.EncodeToString(bytes.Repeat([]byte{0xab}, RootLength)))
	require.NoError(t, err)
	require.Equal(t, repeatedRoot(0xab), root)
	_, err = ParseRoot("0xabcd")
	require.EqualError(t, err, `root "0xabcd" is 2 bytes, want 32`)

	version, err := ParseForkVersion("0x10000020")
	require.NoError(t, err)
	require.Equal(t, ForkVersion{0x10, 0x00, 0x00, 0x20}, version)
	_, err = ParseForkVersion("0xzz")
	require.ErrorContains(t, err, `decode fork version "0xzz"`)
}

func testSeed() common.Seed {
	var seed common.Seed
	for index := range seed {
		seed[index] = byte(0x91 + index)
	}
	return seed
}

func TestVerifyChecksMLDSASignatures(t *testing.T) {
	wallet, err := walletmldsa.NewWalletFromSeed(testSeed())
	require.NoError(t, err)
	publicKey := wallet.GetPK()

	message := Root{0x01}
	signature, err := wallet.Sign(message[:])
	require.NoError(t, err)
	require.NoError(t, Verify(message, publicKey[:], signature[:]))

	tampered := signature
	tampered[0] ^= 0xff
	require.EqualError(t, Verify(message, publicKey[:], tampered[:]), "signature did not verify")
	require.EqualError(t, Verify(Root{0x02}, publicKey[:], signature[:]), "signature did not verify")

	require.ErrorContains(t, Verify(message, publicKey[:], signature[:SignatureLength-1]), "signature must be 4627 bytes, got 4626")
	require.Error(t, Verify(message, publicKey[:PublicKeyLength-1], signature[:]))
}

func TestRandaoCommitmentMatchesQrysm(t *testing.T) {
	seed := testSeed()
	commitment := RandaoCommitment(seed[:])
	require.Equal(t, "3a4c6ce43d45814c5f37ef3d08b4888af27ffbfb8f2870d0229d3fabe941c6b9", hex.EncodeToString(commitment[:]))
}
