package contracts

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGeneratedBindingMatchesABI catches regenerating the Hyperion artifacts
// without rerunning abigen: the checked-in ABI and the embedded one must
// describe the same contract. abigen re-marshals internalType without spaces,
// so that documentation-only field is normalized before comparing.
func TestGeneratedBindingMatchesABI(t *testing.T) {
	artifact, err := os.ReadFile("testdata/EventEmitter.abi")
	require.NoError(t, err)
	require.Equal(t, normalizedABI(t, string(artifact)), normalizedABI(t, EventEmitterMetaData.ABI))
}

func normalizedABI(t *testing.T, payload string) any {
	t.Helper()
	var document any
	require.NoError(t, json.Unmarshal([]byte(payload), &document))

	var walk func(node any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			if internalType, ok := value["internalType"].(string); ok {
				value["internalType"] = strings.ReplaceAll(internalType, " ", "")
			}
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(document)

	return document
}
