package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/stretchr/testify/require"
)

type commandCall struct {
	name      string
	arguments []string
}

func TestPrepareGQRLBuildsPinnedHostToolOutsideLinux(t *testing.T) {
	for name, testCase := range map[string]struct {
		goos    string
		mode    runMode
		backend devnet.Backend
		image   string
	}{
		"non-Linux provision":  {goos: "darwin", mode: provisionNetwork, backend: devnet.BackendDocker, image: "go-qrl:configured"},
		"Kubernetes provision": {goos: "linux", mode: provisionNetwork, backend: devnet.BackendKubernetes, image: "go-qrl:configured"},
		"attached network":     {goos: "linux", mode: useExistingNetwork, backend: devnet.BackendDocker},
		"custom parameters":    {goos: "linux", mode: provisionNetwork, backend: devnet.BackendDocker},
	} {
		t.Run(name, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "bin", "gqrl")
			testsDir := "/workspace/qrl-tests"
			var calls []commandCall
			run := func(_ context.Context, name string, arguments ...string) ([]byte, error) {
				calls = append(calls, commandCall{name: name, arguments: slices.Clone(arguments)})
				switch arguments[2] {
				case "list":
					return []byte("v0.3.1 github.com/cyyber/go-qrl@v0.3.2-pinned\n"), nil
				case "build":
					return nil, os.WriteFile(arguments[4], []byte("gqrl"), 0o755)
				default:
					return nil, nil
				}
			}

			require.NoError(t, prepareGQRL(
				t.Context(),
				testCase.goos,
				testCase.mode,
				testCase.backend,
				testsDir,
				testCase.image,
				destination,
				run,
			))
			require.Len(t, calls, 5)
			buildModule := calls[1].arguments[1]
			require.Equal(t, []commandCall{
				{name: "go", arguments: []string{
					"-C", testsDir,
					"list", "-m",
					"-f", "{{.Version}} {{with .Replace}}{{.Path}}@{{.Version}}{{end}}",
					"github.com/theQRL/go-qrl",
				}},
				{name: "go", arguments: []string{"-C", buildModule, "mod", "init", "gqrlbuild"}},
				{name: "go", arguments: []string{
					"-C", buildModule,
					"mod", "edit",
					"-replace", "github.com/theQRL/go-qrl=github.com/cyyber/go-qrl@v0.3.2-pinned",
				}},
				{name: "go", arguments: []string{"-C", buildModule, "get", "github.com/theQRL/go-qrl/cmd/gqrl@v0.3.1"}},
				{name: "go", arguments: []string{
					"-C", buildModule,
					"build",
					"-o", destination,
					"github.com/theQRL/go-qrl/cmd/gqrl",
				}},
			}, calls)
			require.NoDirExists(t, buildModule)
		})
	}
}

func TestBuildGQRLKeepsTheModuleUnreplacedWhenTheTestsModuleDoesNot(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "bin", "gqrl")
	var calls []commandCall
	run := func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		calls = append(calls, commandCall{name: name, arguments: slices.Clone(arguments)})
		if arguments[2] == "list" {
			return []byte("v0.3.1 \n"), nil
		}
		return nil, nil
	}

	require.NoError(t, buildGQRL(t.Context(), "/workspace/qrl-tests", destination, run))
	require.Len(t, calls, 4)
	buildModule := calls[1].arguments[1]
	require.Equal(t, []commandCall{
		{name: "go", arguments: []string{
			"-C", "/workspace/qrl-tests",
			"list", "-m",
			"-f", "{{.Version}} {{with .Replace}}{{.Path}}@{{.Version}}{{end}}",
			"github.com/theQRL/go-qrl",
		}},
		{name: "go", arguments: []string{"-C", buildModule, "mod", "init", "gqrlbuild"}},
		{name: "go", arguments: []string{"-C", buildModule, "get", "github.com/theQRL/go-qrl/cmd/gqrl@v0.3.1"}},
		{name: "go", arguments: []string{
			"-C", buildModule,
			"build",
			"-o", destination,
			"github.com/theQRL/go-qrl/cmd/gqrl",
		}},
	}, calls)
}

func TestBuildGQRLFailsWhenThePinIsUnreadable(t *testing.T) {
	var calls int
	run := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		calls++
		return nil, errors.New("list failed")
	}

	err := buildGQRL(t.Context(), "/workspace/qrl-tests", filepath.Join(t.TempDir(), "gqrl"), run)
	require.ErrorContains(t, err, "read pinned go-qrl module: list failed")
	require.Equal(t, 1, calls)
}

func TestBuildGQRLFailsWhenThePinCarriesNoVersion(t *testing.T) {
	run := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("\n"), nil
	}

	err := buildGQRL(t.Context(), "/workspace/qrl-tests", filepath.Join(t.TempDir(), "gqrl"), run)
	require.ErrorContains(t, err, "github.com/theQRL/go-qrl reports no version")
}

func TestBuildGQRLRemovesTheBuildModuleAfterAFailure(t *testing.T) {
	var buildModule string
	run := func(_ context.Context, _ string, arguments ...string) ([]byte, error) {
		switch arguments[2] {
		case "list":
			return []byte("v0.3.1 github.com/cyyber/go-qrl@v0.3.2-pinned\n"), nil
		case "build":
			buildModule = arguments[1]
			return nil, errors.New("build failed")
		default:
			return nil, nil
		}
	}

	err := buildGQRL(t.Context(), "/workspace/qrl-tests", filepath.Join(t.TempDir(), "gqrl"), run)
	require.ErrorContains(t, err, "build gqrl: build failed")
	require.NotEmpty(t, buildModule)
	require.NoDirExists(t, buildModule)
}

func TestPrepareGQRLExtractsImageToolOnLinux(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "bin", "gqrl")
	var commands []string
	run := func(_ context.Context, _ string, arguments ...string) ([]byte, error) {
		commands = append(commands, arguments[0])
		switch arguments[0] {
		case "create":
			return []byte("container-id\n"), nil
		case "cp":
			return nil, os.WriteFile(arguments[2], []byte("gqrl"), 0o755)
		case "rm":
			return nil, nil
		default:
			return nil, errors.New("unexpected command")
		}
	}

	require.NoError(t, prepareGQRL(
		t.Context(),
		"linux",
		provisionNetwork,
		devnet.BackendDocker,
		"/workspace/qrl-tests",
		"registry.example/go-qrl:exact",
		destination,
		run,
	))
	require.Equal(t, []string{"create", "cp", "rm"}, commands)
}

func TestExtractGQRLCopiesFromImageAndRemovesContainer(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "bin", "gqrl")
	var calls []commandCall
	run := func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		calls = append(calls, commandCall{name: name, arguments: slices.Clone(arguments)})
		switch arguments[0] {
		case "create":
			return []byte("container-id\n"), nil
		case "cp":
			return nil, os.WriteFile(arguments[2], []byte("gqrl"), 0o600)
		case "rm":
			return nil, nil
		default:
			t.Fatalf("unexpected command: %s %v", name, arguments)
			return nil, nil
		}
	}

	require.NoError(t, extractGQRL(t.Context(), "registry.example/go-qrl@sha256:digest", destination, run))
	require.Equal(t, []commandCall{
		{name: "docker", arguments: []string{"create", "--pull=missing", "registry.example/go-qrl@sha256:digest"}},
		{name: "docker", arguments: []string{"cp", "container-id:/usr/local/bin/gqrl", destination}},
		{name: "docker", arguments: []string{"rm", "-f", "container-id"}},
	}, calls)
	info, err := os.Stat(destination)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

func TestExtractGQRLCleansUpAfterCopyFailure(t *testing.T) {
	var commands []string
	run := func(_ context.Context, _ string, arguments ...string) ([]byte, error) {
		commands = append(commands, arguments[0])
		switch arguments[0] {
		case "create":
			return []byte("container-id\n"), nil
		case "cp":
			return nil, errors.New("copy failed")
		case "rm":
			return nil, nil
		default:
			return nil, nil
		}
	}

	err := extractGQRL(t.Context(), "go-qrl:test", filepath.Join(t.TempDir(), "gqrl"), run)
	require.ErrorContains(t, err, "copy /usr/local/bin/gqrl")
	require.Equal(t, []string{"create", "cp", "rm"}, commands)
}

func TestExtractGQRLCleansUpAfterChmodFailure(t *testing.T) {
	var commands []string
	run := func(_ context.Context, _ string, arguments ...string) ([]byte, error) {
		commands = append(commands, arguments[0])
		if arguments[0] == "create" {
			return []byte("container-id\n"), nil
		}
		return nil, nil
	}

	err := extractGQRL(t.Context(), "go-qrl:test", filepath.Join(t.TempDir(), "gqrl"), run)
	require.ErrorContains(t, err, "make gqrl executable")
	require.Equal(t, []string{"create", "cp", "rm"}, commands)
}

func TestExtractGQRLIncludesCleanupFailure(t *testing.T) {
	run := func(_ context.Context, _ string, arguments ...string) ([]byte, error) {
		switch arguments[0] {
		case "create":
			return []byte("container-id\n"), nil
		case "cp":
			return nil, errors.New("copy failed")
		case "rm":
			return nil, errors.New("remove failed")
		default:
			return nil, nil
		}
	}

	err := extractGQRL(t.Context(), "go-qrl:test", filepath.Join(t.TempDir(), "gqrl"), run)
	require.ErrorContains(t, err, "copy failed")
	require.ErrorContains(t, err, "remove failed")
}
