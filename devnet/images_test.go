package devnet

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImagesResolveDefaults(t *testing.T) {
	images, err := Images{Execution: " registry.example/go-qrl:test ", Clef: "  "}.resolve()
	require.NoError(t, err)
	require.Equal(t, "registry.example/go-qrl:test", images.Execution)
	require.Equal(t, DefaultClefImage, images.Clef)
	require.Equal(t, DefaultConsensusImage, images.Consensus)

	images, err = Images{}.resolve()
	require.NoError(t, err)
	require.Equal(t, DefaultImages(), images)
}

func TestImagesResolveReferences(t *testing.T) {
	digest := "sha256:" + strings.Repeat("0af1", 16)
	for name, reference := range map[string]string{
		"bare repository":    "alpine",
		"tagged":             "local/go-qrl:devnet",
		"uppercase tag":      "ghcr.io/cyyber/go-qrl:VM64-rc.1",
		"registry with port": "localhost:5000/qrl/go-qrl:devnet",
		"digest":             "123456789012.dkr.ecr.eu-west-1.amazonaws.com/go-qrl@" + digest,
		"tag and digest":     "registry.example/qrysm-beacon:v1.2.3@" + digest,
		"other algorithm":    "registry.example/go-qrl@blake3:" + strings.Repeat("ab", 16),
	} {
		t.Run(name, func(t *testing.T) {
			images, err := Images{Execution: reference}.resolve()
			require.NoError(t, err)
			require.Equal(t, reference, images.Execution)
		})
	}
}

func TestImagesResolveRejectsInvalid(t *testing.T) {
	for name, reference := range map[string]string{
		"embedded whitespace":  "local/go qrl:devnet",
		"uppercase repository": "local/GO-QRL:devnet",
		"empty tag":            "local/go-qrl:",
		"empty digest":         "local/go-qrl@",
		"digest not hex":       "local/go-qrl@sha256:zz" + strings.Repeat("00", 31),
		"sha256 too short":     "local/go-qrl@sha256:" + strings.Repeat("ab", 16),
		"invalid host":         "registry..example/go-qrl:devnet",
		"invalid port":         "localhost:http/go-qrl:devnet",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Images{Genesis: reference}.resolve()
			require.ErrorContains(t, err, "genesis image")
			require.ErrorContains(t, err, "reference")
		})
	}
}

func TestImagesResolveReportsEveryInvalidReference(t *testing.T) {
	_, err := Images{Execution: "GO-QRL", Validator: "qrysm@sha256:short"}.resolve()
	require.ErrorContains(t, err, "execution image")
	require.ErrorContains(t, err, "validator image")
}
