package lanes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// The parts of a GitHub workflow the tests read.
type workflow struct {
	Env  map[string]string      `yaml:"env"`
	Jobs map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Env      map[string]string `yaml:"env"`
	Strategy struct {
		Matrix map[string]string `yaml:"matrix"`
	} `yaml:"strategy"`
	Steps []struct {
		Name string            `yaml:"name"`
		Env  map[string]string `yaml:"env"`
	} `yaml:"steps"`
}

func readWorkflow(t *testing.T, name string) workflow {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", name))
	require.NoError(t, err)
	var parsed workflow
	require.NoError(t, yaml.Unmarshal(payload, &parsed))
	return parsed
}

// The image scripts build the commit REMOTESIGNER_REF or QRYSM_REF names. Only
// the build steps may set them, to the commit the images job resolved: the
// checkouts have no local branch to resolve a branch name against.
func TestRemoteSignerNightlyBuildsTheResolvedCommits(t *testing.T) {
	nightly := readWorkflow(t, "remote-signer-nightly.yml")
	commits := map[string]string{
		"REMOTESIGNER_REF": "${{ needs.images.outputs.go-remotesigner-sha }}",
		"QRYSM_REF":        "${{ needs.images.outputs.qrysm-sha }}",
	}
	for variable, commit := range commits {
		require.NotContainsf(t, nightly.Env, variable, "the workflow sets %s for every step", variable)
		steps := 0
		for name, job := range nightly.Jobs {
			require.NotContainsf(t, job.Env, variable, "job %s sets %s for every step", name, variable)
			for _, step := range job.Steps {
				if value, ok := step.Env[variable]; ok {
					require.Equalf(t, commit, value, "step %q", step.Name)
					steps++
				}
			}
		}
		require.Equalf(t, 1, steps, "steps that set %s", variable)
	}
}

// The remote signer nightly runs the lanes the nightly leaves out, each on its
// lane's profile, which the workflow names in a map of its own.
func TestRemoteSignerNightlyRunsLanesOnTheirProfiles(t *testing.T) {
	job := readWorkflow(t, "remote-signer-nightly.yml").Jobs["e2e"]

	var profiles map[string]devnet.Profile
	requireJSONLiteral(t, job.Env["DEVNET_PROFILE"], &profiles)
	for name := range profiles {
		_, err := Named(name)
		require.NoError(t, err)
	}
	for _, lane := range All() {
		profile, ok := profiles[lane.Name]
		require.Equalf(t, lane.LocalImages, ok, "the workflow's profiles name lane %s", lane.Name)
		if ok {
			require.Equalf(t, lane.Profile, profile, "lane %s", lane.Name)
		}
	}

	// Without the variable or the input, only the lanes a qrl-package can
	// start today.
	var defaults []string
	requireJSONLiteral(t, job.Strategy.Matrix["lane"], &defaults)
	require.Equal(t, []string{"consensus-remote-signer", "consensus-remote-signer-exits"}, defaults)
}

// requireJSONLiteral decodes the one quoted JSON literal of a workflow
// expression.
func requireJSONLiteral(t *testing.T, expression string, target any) {
	t.Helper()
	literals := regexp.MustCompile(`'([^']*)'`).FindAllStringSubmatch(expression, -1)
	require.Lenf(t, literals, 1, "literals in %q", expression)
	require.NoError(t, json.Unmarshal([]byte(literals[0][1]), target))
}

// The workflows install Kurtosis through one action, whose version is the one
// the Makefile's preflight wants.
func TestWorkflowsInstallKurtosisThroughTheAction(t *testing.T) {
	root := repositoryRoot(t)
	workflows, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	require.NoError(t, err)
	require.NotEmpty(t, workflows)
	for _, path := range workflows {
		payload, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NotContainsf(t, string(payload), "kurtosis-cli-release-artifacts", "%s downloads Kurtosis itself", filepath.Base(path))
	}

	payload, err := os.ReadFile(filepath.Join(root, ".github", "actions", "install-kurtosis", "action.yml"))
	require.NoError(t, err)
	var action struct {
		Runs struct {
			Steps []struct {
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	require.NoError(t, yaml.Unmarshal(payload, &action))
	require.NotEmpty(t, action.Runs.Steps)
	version := action.Runs.Steps[0].Env["KURTOSIS_VERSION"]
	require.Regexp(t, `^\d+\.\d+\.\d+$`, version)

	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	require.NoError(t, err)
	preflight := regexp.MustCompile(`CLI Version:\[\[:space:\]\]\+(\d+)\\\.(\d+)\\\.`).FindStringSubmatch(string(makefile))
	require.Len(t, preflight, 3, "the Makefile's Kurtosis version check")
	require.Truef(t, strings.HasPrefix(version, preflight[1]+"."+preflight[2]+"."), "Kurtosis %s, the preflight wants %s.%s.x", version, preflight[1], preflight[2])
}
