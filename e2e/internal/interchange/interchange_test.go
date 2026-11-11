package interchange

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	testRoot      = "0x" + strings.Repeat("ab", 32)
	testPublicKey = "0x" + strings.Repeat("cd", 2592)
	otherKey      = "0x" + strings.Repeat("ef", 2592)
)

func TestConsecutiveAttestationsEncodesEIP3076(t *testing.T) {
	history, err := ConsecutiveAttestations(testRoot, testPublicKey, 5, 7)
	require.NoError(t, err)
	encoded, err := history.Encode()
	require.NoError(t, err)

	// Epochs are decimal strings and the signing roots are left out, as
	// Qrysm's validator/slashing-protection-history/format reads them.
	require.JSONEq(t, `{
		"metadata": {"interchange_format_version": "5", "genesis_validators_root": "`+testRoot+`"},
		"data": [{
			"pubkey": "`+testPublicKey+`",
			"signed_blocks": [],
			"signed_attestations": [
				{"source_epoch": "4", "target_epoch": "5"},
				{"source_epoch": "5", "target_epoch": "6"},
				{"source_epoch": "6", "target_epoch": "7"}
			]
		}]
	}`, encoded)

	parsed, err := Parse(encoded)
	require.NoError(t, err)
	require.Equal(t, history, parsed)
}

func TestConsecutiveAttestationsRejectsInvalidInput(t *testing.T) {
	for _, test := range []struct {
		name        string
		root        string
		publicKey   string
		first, last uint64
		wantErr     string
	}{
		{name: "short root", root: "0xabcd", publicKey: testPublicKey, first: 1, last: 1,
			wantErr: "genesis validators root is 2 bytes, want 32"},
		{name: "root not hex", root: "0x" + strings.Repeat("zz", 32), publicKey: testPublicKey, first: 1, last: 1,
			wantErr: "decode genesis validators root"},
		{name: "public key of another scheme", root: testRoot, publicKey: "0x" + strings.Repeat("cd", 48), first: 1, last: 1,
			wantErr: "public key is 48 bytes, want 2592"},
		{name: "target without a source", root: testRoot, publicKey: testPublicKey, first: 0, last: 1,
			wantErr: "the first target epoch must be after epoch 0"},
		{name: "last before first", root: testRoot, publicKey: testPublicKey, first: 3, last: 2,
			wantErr: "the last target epoch 2 is before the first, 3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ConsecutiveAttestations(test.root, test.publicKey, test.first, test.last)
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestParseReadsQrysmExport(t *testing.T) {
	// Qrysm exports a key's attestations by source epoch, with the signing
	// roots of those it signed itself.
	history, err := Parse(`{
		"metadata": {"interchange_format_version": "5", "genesis_validators_root": "` + testRoot + `"},
		"data": [
			{"pubkey": "` + strings.ToUpper(testPublicKey[2:]) + `", "signed_blocks": [{"slot": "81", "signing_root": "0x01"}],
			 "signed_attestations": [{"source_epoch": "4", "target_epoch": "5"}]},
			{"pubkey": "` + otherKey + `", "signed_blocks": [],
			 "signed_attestations": [{"source_epoch": "1", "target_epoch": "2"}]},
			{"pubkey": "` + testPublicKey + `", "signed_blocks": [],
			 "signed_attestations": [{"source_epoch": "7", "target_epoch": "8", "signing_root": "0x02"}]}
		]
	}`)
	require.NoError(t, err)
	require.Equal(t, []Block{{Slot: 81, SigningRoot: "0x01"}}, history.Data[0].SignedBlocks)
	require.Equal(t, []Attestation{
		{SourceEpoch: 4, TargetEpoch: 5},
		{SourceEpoch: 7, TargetEpoch: 8, SigningRoot: "0x02"},
	}, history.Attestations(testPublicKey))
	require.Empty(t, history.Attestations("0x"+strings.Repeat("12", 2592)))
}

func TestParseRejectsInvalidHistories(t *testing.T) {
	_, err := Parse(`{"metadata": {"interchange_format_version": "4"}, "data": []}`)
	require.EqualError(t, err, `slashing-protection history has format version "4", want "5"`)

	_, err = Parse(`{"metadata": {"interchange_format_version": "5"}, "data": [{"signed_attestations": [{"target_epoch": 5}]}]}`)
	require.ErrorContains(t, err, "decode slashing-protection history")
}

func TestSameHex(t *testing.T) {
	require.True(t, SameHex("0xAB", "ab"))
	require.True(t, SameHex("ab", "0xab"))
	require.False(t, SameHex("0xab", "0xac"))
}
