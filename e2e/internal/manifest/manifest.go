// Package manifest defines the file the E2E runner writes for live test
// suites: which lane ran, under which profile, against which network.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cyyber/qrl-tests/devnet"
)

const (
	FileName = "manifest.json"
	PathEnv  = "QRL_TEST_MANIFEST"
)

type Manifest struct {
	Lane        string             `json:"lane,omitempty"`
	Profile     devnet.Profile     `json:"profile,omitempty"`
	Environment devnet.Environment `json:"environment"`
}

func Write(path string, manifest Manifest) error {
	if _, err := manifest.Environment.Primary(); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode test manifest: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create test manifest directory: %w", err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		return fmt.Errorf("write test manifest: %w", err)
	}
	return nil
}

func Read(path string) (Manifest, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read test manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode test manifest: %w", err)
	}

	if _, err := manifest.Environment.Primary(); err != nil {
		return Manifest{}, fmt.Errorf("test manifest %s: %w", path, err)
	}
	return manifest, nil
}

func FromEnv() (Manifest, error) {
	path := os.Getenv(PathEnv)
	if path == "" {
		return Manifest{}, fmt.Errorf("%s is not configured", PathEnv)
	}
	return Read(path)
}
