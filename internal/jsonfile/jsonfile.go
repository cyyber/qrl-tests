// Package jsonfile persists report documents as indented JSON files.
package jsonfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Write encodes value as indented JSON and writes it to path, creating the
// parent directory when needed. The label names the document in errors.
func Write(path string, value any, label string) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", label, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s directory: %w", label, err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", label, err)
	}
	return nil
}
