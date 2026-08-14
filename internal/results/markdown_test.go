package results

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onsi/ginkgo/v2/types"
	"github.com/stretchr/testify/require"
)

func TestSummarizeWritesMarkdown(t *testing.T) {
	root := t.TempDir()
	summary, err := Summarize(root, []Outcome{{
		Name: "execution",
		Observation: Observation{reports: []types.Report{{
			SuiteDescription: "ABI E2E suite",
			SuiteSucceeded:   true,
			SpecReports: []types.SpecReport{{
				LeafNodeType: types.NodeTypeIt,
				State:        types.SpecStatePassed,
			}},
		}}},
	}})
	require.NoError(t, err)

	markdown, err := os.ReadFile(filepath.Join(root, MarkdownFileName))
	require.NoError(t, err)
	require.Equal(t, summary.markdown(), string(markdown))
}

func TestMarkdownPassingSuites(t *testing.T) {
	summary := Summary{
		Result: "passed",
		Lanes: []LaneSummary{{
			Name:  "execution",
			Class: ClassPassed,
			suites: []suiteSummary{
				{Name: "ABI E2E suite", Class: ClassPassed, Counts: Counts{Specs: 2, Passed: 2}},
				{Name: "API E2E suite", Class: ClassPassed, Counts: Counts{Specs: 3, Passed: 3}},
			},
		}},
	}

	want := `## E2E: passed

### execution

| Suite | Result |
| --- | ---: |
| ABI E2E suite | 2/2 |
| API E2E suite | 3/3 |
`
	require.Equal(t, want, summary.markdown())
}

func TestMarkdownFailedSuiteDetails(t *testing.T) {
	summary := Summary{
		Result: "failed",
		Lanes: []LaneSummary{{
			Name:  "execution",
			Class: ClassAssertion,
			Error: "exit status 1",
			suites: []suiteSummary{{
				Name:  "ABI E2E suite",
				Class: ClassAssertion,
				Counts: Counts{
					Specs:  2,
					Passed: 1,
					Failed: 1,
				},
				Failures: []Failure{{
					Spec:     "ABI decodes events",
					State:    "failed",
					Message:  "expected 1,\ngot 2",
					Location: "calls_test.go:12",
				}},
				SuiteFailures:   []string{"suite cleanup failed"},
				UnexpectedSkips: []string{"ABI encodes arrays"},
			}},
		}},
	}

	want := strings.Join([]string{
		"## E2E: failed",
		"",
		"### execution",
		"",
		"| Suite | Result |",
		"| --- | ---: |",
		"| ABI E2E suite | 1/2 failed |",
		"",
		"#### ABI E2E suite failures",
		"",
		"- **failed** `ABI decodes events` (calls_test.go:12)",
		"  expected 1,",
		"  got 2",
		"- **suite** suite cleanup failed",
		"- **skipped** `ABI encodes arrays`",
		"",
	}, "\n")
	require.Equal(t, want, summary.markdown())
}

func TestMarkdownInfrastructureFailureWithoutSuites(t *testing.T) {
	summary := Summary{
		Result: "failed",
		Lanes: []LaneSummary{{
			Name:  "consensus",
			Class: ClassInfrastructure,
			Error: "network did not become ready\nconsensus REST unavailable",
		}},
	}

	want := strings.Join([]string{
		"## E2E: failed",
		"",
		"### consensus",
		"",
		"**Result:** error",
		"",
		"### consensus details",
		"",
		"```",
		"network did not become ready",
		"consensus REST unavailable",
		"```",
		"",
	}, "\n")
	require.Equal(t, want, summary.markdown())
}
