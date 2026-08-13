package devnet

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImagesResolveDefaults(t *testing.T) {
	images, err := Images{Execution: " registry.example/go-qrl:test ", Clef: "  "}.Resolved()
	require.NoError(t, err)
	require.Equal(t, "registry.example/go-qrl:test", images.Execution)
	require.Equal(t, DefaultClefImage, images.Clef)
	require.Equal(t, DefaultConsensusImage, images.Consensus)

	images, err = Images{}.Resolved()
	require.NoError(t, err)
	require.Equal(t, DefaultImages(), images)
}

func TestImagesResolveValidReferences(t *testing.T) {
	digest := "sha256:" + strings.Repeat("0af1", 16)
	for name, reference := range map[string]string{
		"tag":            "local/go-qrl:devnet",
		"digest":         "ghcr.io/example/go-qrl@" + digest,
		"tag and digest": "registry.example/qrysm-beacon:v1.2.3@" + digest,
	} {
		t.Run(name, func(t *testing.T) {
			images, err := Images{Execution: reference}.Resolved()
			require.NoError(t, err)
			require.Equal(t, reference, images.Execution)
		})
	}
}

func TestImagesResolveRejectsInvalid(t *testing.T) {
	for name, reference := range map[string]string{
		"embedded whitespace": "local/go qrl:devnet",
		"malformed digest":    "local/go-qrl@sha256:short",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Images{Genesis: reference}.Resolved()
			require.ErrorContains(t, err, "genesis image")
			require.ErrorContains(t, err, "reference")
		})
	}
}

func TestImagesResolveReportsEveryInvalidReference(t *testing.T) {
	_, err := Images{Execution: "GO-QRL", Validator: "qrysm@sha256:short"}.Resolved()
	require.ErrorContains(t, err, "execution image")
	require.ErrorContains(t, err, "validator image")
}
