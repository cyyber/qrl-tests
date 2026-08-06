package lanes

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistry(t *testing.T) {
	root := repositoryRoot(t)
	registered := make(map[SuiteID]struct{})
	for _, id := range RegisteredSuites() {
		require.NotEmpty(t, id)
		require.NotEmpty(t, id.Package())
		registered[id] = struct{}{}
	}
	seen := make(map[string]struct{})
	for _, lane := range All() {
		require.NotEmpty(t, lane.Name)
		_, duplicate := seen[lane.Name]
		require.Falsef(t, duplicate, "duplicate lane %q", lane.Name)
		seen[lane.Name] = struct{}{}
		require.NotEmpty(t, lane.Profile)
		require.NotEmpty(t, lane.Suites)
		require.Positive(t, lane.Timeout)
		for index, pattern := range lane.Packages() {
			_, ok := registered[lane.Suites[index]]
			require.Truef(t, ok, "lane %s references unknown suite %q", lane.Name, lane.Suites[index])
			path := strings.TrimSuffix(strings.TrimPrefix(pattern, "./"), "/...")
			info, err := os.Stat(filepath.Join(root, path))
			require.NoErrorf(t, err, "lane %s package %s", lane.Name, pattern)
			require.Truef(t, info.IsDir(), "lane %s package %s is not a directory", lane.Name, pattern)
		}
	}
}

func TestLaneSelect(t *testing.T) {
	executionABI, err := Named("execution-abi")
	require.NoError(t, err)

	selected, err := executionABI.Select([]string{"execution-abi", "execution-abi"})
	require.NoError(t, err)
	require.Equal(t, []SuiteID{SuiteExecutionABI}, selected.Suites)

	_, err = executionABI.Select([]string{"unknown"})
	require.ErrorContains(t, err, "unknown E2E suite")
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "../../.."))
}
