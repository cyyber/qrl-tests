package validatorops

import (
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/signing"
	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/common/hexutil"
)

func TestSignDoubleVote(t *testing.T) {
	keys := map[uint64]*Key{}
	signer := func(index uint64) Signer {
		key, err := DeterministicKey(0x50 + byte(index))
		require.NoError(t, err)
		keys[index] = key
		return Signer{Index: index, Key: key}
	}
	fork, root := signing.ForkVersion{0x10, 0, 0, 0x38}, signing.Root{1, 2, 3}

	// Validator 7 votes twice; 9 and 4 once each.
	slashing, err := SignDoubleVote([]Signer{signer(9), signer(7)}, []Signer{signer(7), signer(4)}, 40, 5, fork, root)
	require.NoError(t, err)
	require.Equal(t, []uint64{7, 9}, slashing.Attestation1.AttestingIndices)
	require.Equal(t, []uint64{4, 7}, slashing.Attestation2.AttestingIndices)

	domain := signing.ComputeDomain(signing.DomainBeaconAttester, fork, root)
	for which, attestation := range []struct {
		indices    []uint64
		signatures []string
		head       signing.Root
	}{
		{slashing.Attestation1.AttestingIndices, slashing.Attestation1.Signatures, signing.Root{1}},
		{slashing.Attestation2.AttestingIndices, slashing.Attestation2.Signatures, signing.Root{2}},
	} {
		data := signing.AttestationData{Slot: 40, BeaconBlockRoot: attestation.head, Target: signing.Checkpoint{Epoch: 5}}
		signingRoot := signing.SigningRoot(data.HashTreeRoot(), domain)
		require.Len(t, attestation.signatures, len(attestation.indices))
		for position, index := range attestation.indices {
			signature, err := hexutil.Decode(attestation.signatures[position])
			require.NoError(t, err)
			require.NoError(t, signing.Verify(signingRoot, keys[index].PublicKey(), signature), "vote %d of validator %d", which+1, index)
		}
	}
	require.Equal(t, uint64(5), slashing.Attestation1.Data.Target.Epoch)
	require.Equal(t, slashing.Attestation1.Data.Target, slashing.Attestation2.Data.Target)
	require.NotEqual(t, slashing.Attestation1.Data.BeaconBlockRoot, slashing.Attestation2.Data.BeaconBlockRoot)

	_, err = SignDoubleVote([]Signer{signer(7), signer(7)}, []Signer{signer(7)}, 40, 5, fork, root)
	require.ErrorContains(t, err, "validator 7 signs twice")
	_, err = SignDoubleVote(nil, []Signer{signer(7)}, 40, 5, fork, root)
	require.ErrorContains(t, err, "needs a signer")
}
