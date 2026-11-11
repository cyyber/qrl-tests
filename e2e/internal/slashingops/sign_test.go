package slashingops

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/beacon"
	"github.com/cyyber/qrl-tests/e2e/internal/signing"
	"github.com/cyyber/qrl-tests/e2e/internal/validatorops"
	"github.com/stretchr/testify/require"
)

func hexRoot(value byte) string {
	return "0x" + strings.Repeat(hex.EncodeToString([]byte{value}), signing.RootLength)
}

func testSigner(t *testing.T) validatorops.Signer {
	t.Helper()
	key, err := validatorops.DeterministicKey(0x91)
	require.NoError(t, err)
	return validatorops.Signer{
		Key:                   key,
		Fork:                  signing.Fork{CurrentVersion: signing.ForkVersion{0x10, 0x00, 0x00, 0x20}},
		GenesisValidatorsRoot: signing.Root{0x77},
	}
}

func TestSignAttestation(t *testing.T) {
	signer := testSigner(t)
	data := beacon.AttestationData{
		Slot: 77, CommitteeIndex: 2, BeaconBlockRoot: hexRoot(0x44),
		Source: beacon.Checkpoint{Epoch: 7, Root: hexRoot(0x55)},
		Target: beacon.Checkpoint{Epoch: 9, Root: hexRoot(0x66)},
	}
	duty := beacon.AttesterDuty{Slot: 77, CommitteeIndex: 2, CommitteeLength: 10, ValidatorCommitteeIndex: 9}

	attestation, err := SignAttestation(signer, duty, data)
	require.NoError(t, err)
	require.Equal(t, data, attestation.Data)
	require.Equal(t, "0x0006", attestation.AggregationBits)
	require.Len(t, attestation.Signatures, 1)

	// Qrysm computes this root for the data.
	root, err := attestationDataRoot(data)
	require.NoError(t, err)
	require.Equal(t, "df9178b2a27c5067fb0dc9508e5a11c400884df4ebcff6a45c5b1be1a1b9df78", hex.EncodeToString(root[:]))
	domain := signer.Fork.Domain(signing.DomainBeaconAttester, 9, signer.GenesisValidatorsRoot)
	signature, err := hex.DecodeString(strings.TrimPrefix(attestation.Signatures[0], "0x"))
	require.NoError(t, err)
	require.NoError(t, signing.Verify(signing.SigningRoot(root, domain), signer.Key.PublicKey(), signature))

	data.Target.Root = "0x66"
	_, err = SignAttestation(signer, duty, data)
	require.EqualError(t, err, `attestation of slot 77: root "0x66" is 1 bytes, want 32`)
}

func TestSignAttesterSlashing(t *testing.T) {
	signer := testSigner(t)
	first := beacon.AttestationData{
		Slot: 77, CommitteeIndex: 2, BeaconBlockRoot: hexRoot(0x44),
		Source: beacon.Checkpoint{Epoch: 7, Root: hexRoot(0x55)},
		Target: beacon.Checkpoint{Epoch: 9, Root: hexRoot(0x66)},
	}
	second := first
	second.BeaconBlockRoot = hexRoot(0x45)

	slashing, err := SignAttesterSlashing(signer, 64, first, second)
	require.NoError(t, err)
	domain := signer.Fork.Domain(signing.DomainBeaconAttester, 9, signer.GenesisValidatorsRoot)
	for _, attestation := range []beacon.SignedIndexedAttestation{slashing.Attestation1, slashing.Attestation2} {
		require.Equal(t, []string{"64"}, attestation.AttestingIndices)
		require.Len(t, attestation.Signatures, 1)
		root, err := attestationDataRoot(attestation.Data)
		require.NoError(t, err)
		signature, err := hex.DecodeString(strings.TrimPrefix(attestation.Signatures[0], "0x"))
		require.NoError(t, err)
		require.NoError(t, signing.Verify(signing.SigningRoot(root, domain), signer.Key.PublicKey(), signature))
	}
	require.Equal(t, first, slashing.Attestation1.Data)
	require.Equal(t, second, slashing.Attestation2.Data)
	require.True(t, IsDoubleVote(slashing.Attestation1.Data, slashing.Attestation2.Data))
}

func TestSignProposerSlashing(t *testing.T) {
	signer := testSigner(t)
	key := signer.Key
	first := beacon.BeaconBlockHeaderMessage{
		Slot: 123, ProposerIndex: 45, ParentRoot: hexRoot(0x11), StateRoot: hexRoot(0x22), BodyRoot: hexRoot(0x33),
	}
	second := first
	second.BodyRoot = hexRoot(0x44)

	slashing, err := SignProposerSlashing(signer, 8, first, second)
	require.NoError(t, err)
	require.Equal(t, first, slashing.SignedHeader1.Message)
	require.Equal(t, second, slashing.SignedHeader2.Message)

	// Qrysm computes this root for the first header.
	root, err := headerRoot(first)
	require.NoError(t, err)
	require.Equal(t, "43d88b69660aef4a5898e981591de4f5114841ab7e04c312212bbb8d0fe0c85a", hex.EncodeToString(root[:]))
	domain := signer.Fork.Domain(signing.DomainBeaconProposer, 15, signer.GenesisValidatorsRoot)
	signature, err := hex.DecodeString(strings.TrimPrefix(slashing.SignedHeader1.Signature, "0x"))
	require.NoError(t, err)
	require.NoError(t, signing.Verify(signing.SigningRoot(root, domain), key.PublicKey(), signature))

	second.ParentRoot = "0x01"
	_, err = SignProposerSlashing(signer, 8, first, second)
	require.EqualError(t, err, `header of slot 123: root "0x01" is 1 bytes, want 32`)
}
