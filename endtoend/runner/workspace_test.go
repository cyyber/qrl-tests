// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
)

func TestPrepareWorkspace(t *testing.T) {
	testsDir := testModule(t, qrlTestsModule, "1.26.0")
	sourceDir := testModule(t, goQRLModule, "1.26.5")
	path, err := prepareWorkspace(t.TempDir(), testsDir, sourceDir)
	require.NoError(t, err)

	payload, err := os.ReadFile(path)
	require.NoError(t, err)
	workspace, err := modfile.ParseWork(path, payload, nil)
	require.NoError(t, err)
	require.Equal(t, "1.26.5", workspace.Go.Version)
	require.Len(t, workspace.Use, 2)
	require.Equal(t, filepath.ToSlash(testsDir), workspace.Use[0].Path)
	require.Equal(t, filepath.ToSlash(sourceDir), workspace.Use[1].Path)
}

func TestPrepareWorkspaceRejectsWrongModule(t *testing.T) {
	testsDir := testModule(t, "example.com/not-qrl-tests", "1.26.5")
	sourceDir := testModule(t, goQRLModule, "1.26.5")
	_, err := prepareWorkspace(t.TempDir(), testsDir, sourceDir)
	require.ErrorContains(t, err, qrlTestsModule)
}

func testModule(t *testing.T, module, goVersion string) string {
	t.Helper()
	directory := t.TempDir()
	payload := []byte("module " + module + "\n\ngo " + goVersion + "\n")
	require.NoError(t, os.WriteFile(filepath.Join(directory, "go.mod"), payload, 0o600))
	return directory
}
