// Package manifest defines the file the E2E runner writes for live test
// suites: which lane ran, under which profile, against which network.
package manifest

import (
	"fmt"
	"os"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/internal/jsonfile"
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

	return jsonfile.Write(path, manifest, "test manifest")
}

func Read(path string) (Manifest, error) {
	manifest, err := jsonfile.Read[Manifest](path, "test manifest")
	if err != nil {
		return Manifest{}, err
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
