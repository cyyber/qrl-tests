// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

package runner

import (
	"fmt"
	"go/version"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

const (
	qrlTestsModule = "github.com/cyyber/qrl-tests"
	goQRLModule    = "github.com/theQRL/go-qrl"
)

func prepareWorkspace(reportRoot, testsDir, sourceDir string) (string, error) {
	if sourceDir == "" {
		return "", nil
	}
	tests, err := readModule(testsDir, qrlTestsModule)
	if err != nil {
		return "", err
	}
	source, err := filepath.Abs(sourceDir)
	if err != nil {
		return "", fmt.Errorf("resolve go-qrl source directory: %w", err)
	}
	goQRL, err := readModule(source, goQRLModule)
	if err != nil {
		return "", err
	}
	goVersion := tests.Go.Version
	if version.Compare("go"+goQRL.Go.Version, "go"+goVersion) > 0 {
		goVersion = goQRL.Go.Version
	}
	payload := []byte(fmt.Sprintf(
		"go %s\n\nuse (\n\t%s\n\t%s\n)\n",
		goVersion,
		strconv.Quote(filepath.ToSlash(testsDir)),
		strconv.Quote(filepath.ToSlash(source)),
	))
	if _, err := modfile.ParseWork("go.work", payload, nil); err != nil {
		return "", fmt.Errorf("construct Go workspace: %w", err)
	}
	directory := filepath.Join(reportRoot, ".workspace")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", fmt.Errorf("create Go workspace directory: %w", err)
	}
	path := filepath.Join(directory, "go.work")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return "", fmt.Errorf("write Go workspace: %w", err)
	}
	return path, nil
}

func readModule(directory, expected string) (*modfile.File, error) {
	payload, err := os.ReadFile(filepath.Join(directory, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("read %s module: %w", expected, err)
	}
	file, err := modfile.Parse("go.mod", payload, nil)
	if err != nil {
		return nil, fmt.Errorf("parse %s module: %w", expected, err)
	}
	if file.Module == nil || file.Module.Mod.Path != expected {
		return nil, fmt.Errorf("%s does not contain module %s", directory, expected)
	}
	if file.Go == nil || strings.TrimSpace(file.Go.Version) == "" {
		return nil, fmt.Errorf("module %s has no Go version", expected)
	}
	return file, nil
}

func setEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}
